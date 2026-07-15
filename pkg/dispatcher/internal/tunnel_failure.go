package dispatcherinternal

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/openai/tunnel-client/pkg/mcpclient"
	"github.com/openai/tunnel-client/pkg/version"
)

type tunnelFailureSource string

const (
	tunnelFailureSourceTargetHTTP      tunnelFailureSource = "target_http"
	tunnelFailureSourceDNS             tunnelFailureSource = "dns"
	tunnelFailureSourceTLS             tunnelFailureSource = "tls"
	tunnelFailureSourceConnect         tunnelFailureSource = "connect"
	tunnelFailureSourceTransportClosed tunnelFailureSource = "transport_closed"
	tunnelFailureSourceTimeout         tunnelFailureSource = "timeout"
	tunnelFailureSourceProtocol        tunnelFailureSource = "protocol"
	tunnelFailureSourceClientInternal  tunnelFailureSource = "client_internal"
)

type tunnelFailure struct {
	Version                  int                 `json:"version"`
	Source                   tunnelFailureSource `json:"source"`
	UpstreamResponseReceived bool                `json:"upstream_response_received"`
	UpstreamStatus           int                 `json:"upstream_status,omitempty"`
}

type tunnelFailureData struct {
	TunnelFailure tunnelFailure `json:"tunnel_failure"`
}

// protocolFailureError marks a response-processing failure as target protocol
// behavior without exposing the underlying payload or exception text.
type protocolFailureError struct {
	cause error
}

func (e *protocolFailureError) Error() string { return "MCP protocol failure" }

func (e *protocolFailureError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newProtocolFailureError(cause error) error {
	return &protocolFailureError{cause: cause}
}

func classifyTunnelFailure(statusCode int, err error) tunnelFailure {
	failure := tunnelFailure{
		Version:                  1,
		Source:                   tunnelFailureSourceClientInternal,
		UpstreamResponseReceived: false,
	}

	// A target-owned HTTP status is stronger evidence than any transport error
	// the MCP SDK may synthesize while interpreting its response body.
	if statusCode >= http.StatusBadRequest && statusCode <= 599 {
		failure.Source = tunnelFailureSourceTargetHTTP
		failure.UpstreamResponseReceived = true
		failure.UpstreamStatus = statusCode
		return failure
	}

	var nonProtocolResponse *mcpclient.NonProtocolResponseError
	if errors.As(err, &nonProtocolResponse) {
		failure.Source = tunnelFailureSourceProtocol
		return failure
	}

	var protocolFailure *protocolFailureError
	if errors.As(err, &protocolFailure) {
		failure.Source = tunnelFailureSourceProtocol
		return failure
	}

	if errors.Is(err, io.ErrClosedPipe) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, net.ErrClosed) ||
		errors.Is(err, mcp.ErrConnectionClosed) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) {
		failure.Source = tunnelFailureSourceTransportClosed
		return failure
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		failure.Source = tunnelFailureSourceDNS
		return failure
	}

	if isTLSFailure(err) {
		failure.Source = tunnelFailureSourceTLS
		return failure
	}

	if errors.Is(err, context.DeadlineExceeded) {
		failure.Source = tunnelFailureSourceTimeout
		return failure
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		failure.Source = tunnelFailureSourceTimeout
		return failure
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) && strings.EqualFold(opErr.Op, "dial") {
		failure.Source = tunnelFailureSourceConnect
		return failure
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		failure.Source = tunnelFailureSourceConnect
		return failure
	}

	return failure
}

func classifyTransportErrorKind(statusCode int, err error) string {
	if statusCode >= http.StatusBadRequest && statusCode <= 599 {
		return "http_status"
	}

	var nonProtocolResponse *mcpclient.NonProtocolResponseError
	if errors.As(err, &nonProtocolResponse) {
		if kind := nonProtocolResponse.Kind(); kind != "" {
			return string(kind)
		}
		return "non_protocol_response"
	}
	var protocolFailure *protocolFailureError
	if errors.As(err, &protocolFailure) {
		return "invalid_protocol_response"
	}

	switch {
	case errors.Is(err, io.ErrClosedPipe), errors.Is(err, syscall.EPIPE):
		return "closed_pipe"
	case errors.Is(err, io.EOF):
		return "eof"
	case errors.Is(err, io.ErrUnexpectedEOF):
		return "unexpected_eof"
	case errors.Is(err, net.ErrClosed), errors.Is(err, mcp.ErrConnectionClosed):
		return "connection_closed"
	case errors.Is(err, syscall.ECONNRESET):
		return "connection_reset"
	case errors.Is(err, syscall.ECONNABORTED):
		return "connection_aborted"
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "dns"
	}
	if isTLSFailure(err) {
		return "tls"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) && timeoutErr.Timeout() {
		return "timeout"
	}

	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection_refused"
	case errors.Is(err, syscall.ENETUNREACH):
		return "network_unreachable"
	case errors.Is(err, syscall.EHOSTUNREACH):
		return "host_unreachable"
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && strings.EqualFold(opErr.Op, "dial") {
		return "dial"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "unknown"
}

func isTLSFailure(err error) bool {
	var verificationErr *tls.CertificateVerificationError
	var recordHeaderErr tls.RecordHeaderError
	var alertErr tls.AlertError
	var unknownAuthorityErr x509.UnknownAuthorityError
	var hostnameErr x509.HostnameError
	var certificateInvalidErr x509.CertificateInvalidError
	var systemRootsErr x509.SystemRootsError
	return errors.As(err, &verificationErr) ||
		errors.As(err, &recordHeaderErr) ||
		errors.As(err, &alertErr) ||
		errors.As(err, &unknownAuthorityErr) ||
		errors.As(err, &hostnameErr) ||
		errors.As(err, &certificateInvalidErr) ||
		errors.As(err, &systemRootsErr)
}

func buildTunnelFailureJSONRPCErrorResponse(req *jsonrpc.Request, statusCode int, failure tunnelFailure) ([]byte, error) {
	if req == nil {
		return nil, errors.New("nil request provided to build tunnel failure response")
	}
	if statusCode == 0 {
		statusCode = http.StatusInternalServerError
	}
	message := http.StatusText(statusCode)
	if message == "" {
		message = "MCP transport error"
	}
	data, err := json.Marshal(tunnelFailureData{TunnelFailure: failure})
	if err != nil {
		return nil, err
	}
	return jsonrpc.EncodeMessage(&jsonrpc.Response{
		ID: req.ID,
		Error: &jsonrpc.Error{
			Code:    jsonrpc.CodeInternalError,
			Message: message,
			Data:    data,
		},
	})
}

func tunnelFailureLogAttrs(failure tunnelFailure, transportErrorKind string) []any {
	attrs := []any{
		slog.String("failure_source", string(failure.Source)),
		slog.String("transport_error_kind", transportErrorKind),
		slog.Bool("upstream_response_received", failure.UpstreamResponseReceived),
		slog.String("tunnel_client_version", version.Version),
	}
	if failure.UpstreamStatus != 0 {
		attrs = append(attrs, slog.Int("upstream_status", failure.UpstreamStatus))
	}
	return attrs
}
