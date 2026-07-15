package dispatcherinternal

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"syscall"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	"github.com/openai/tunnel-client/pkg/mcpclient"
)

func TestClassifyTunnelFailure(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name       string
		statusCode int
		err        error
		want       tunnelFailure
		wantKind   string
	}{
		{
			name:       "target HTTP response wins over body parse failure",
			statusCode: http.StatusMethodNotAllowed,
			err:        errors.New("target body contained a secret"),
			want: tunnelFailure{
				Version:                  1,
				Source:                   tunnelFailureSourceTargetHTTP,
				UpstreamResponseReceived: true,
				UpstreamStatus:           http.StatusMethodNotAllowed,
			},
			wantKind: "http_status",
		},
		{name: "DNS", err: &net.DNSError{Err: "no such host", Name: "private.example"}, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceDNS}, wantKind: "dns"},
		{name: "TLS", err: tls.RecordHeaderError{}, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceTLS}, wantKind: "tls"},
		{name: "connect", err: &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceConnect}, wantKind: "connection_refused"},
		{name: "closed pipe", err: io.ErrClosedPipe, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceTransportClosed}, wantKind: "closed_pipe"},
		{name: "MCP connection closed", err: mcp.ErrConnectionClosed, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceTransportClosed}, wantKind: "connection_closed"},
		{name: "timeout", err: context.DeadlineExceeded, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceTimeout}, wantKind: "timeout"},
		{name: "non-protocol response", err: &mcpclient.NonProtocolResponseError{}, want: tunnelFailure{Version: 1, Source: tunnelFailureSourceProtocol}, wantKind: "non_protocol_response"},
		{name: "protocol", err: newProtocolFailureError(errors.New("private target payload")), want: tunnelFailure{Version: 1, Source: tunnelFailureSourceProtocol}, wantKind: "invalid_protocol_response"},
		{name: "unknown", err: errors.New("private target URL and token"), want: tunnelFailure{Version: 1, Source: tunnelFailureSourceClientInternal}, wantKind: "unknown"},
		{name: "nil", want: tunnelFailure{Version: 1, Source: tunnelFailureSourceClientInternal}, wantKind: "unknown"},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, classifyTunnelFailure(tc.statusCode, tc.err))
			require.Equal(t, tc.wantKind, classifyTransportErrorKind(tc.statusCode, tc.err))
		})
	}
}

func TestBuildTunnelFailureJSONRPCErrorResponse(t *testing.T) {
	t.Parallel()

	id, err := jsonrpc.MakeID("rpc-request")
	require.NoError(t, err)
	payload, err := buildTunnelFailureJSONRPCErrorResponse(
		&jsonrpc.Request{ID: id, Method: "tools/list"},
		http.StatusBadGateway,
		classifyTunnelFailure(0, io.ErrClosedPipe),
	)
	require.NoError(t, err)
	require.NotContains(t, string(payload), io.ErrClosedPipe.Error())
	require.NotContains(t, string(payload), "upstream_status")

	response := decodeJSONRPCResponse(t, payload)
	wireError, ok := response.Error.(*jsonrpc.Error)
	require.True(t, ok)
	require.Equal(t, int64(jsonrpc.CodeInternalError), wireError.Code)
	require.Equal(t, http.StatusText(http.StatusBadGateway), wireError.Message)

	var data tunnelFailureData
	require.NoError(t, json.Unmarshal(wireError.Data, &data))
	require.Equal(t, tunnelFailure{
		Version:                  1,
		Source:                   tunnelFailureSourceTransportClosed,
		UpstreamResponseReceived: false,
	}, data.TunnelFailure)
}

func TestBuildTunnelFailureJSONRPCErrorResponseRejectsNilRequest(t *testing.T) {
	t.Parallel()

	_, err := buildTunnelFailureJSONRPCErrorResponse(nil, http.StatusBadGateway, tunnelFailure{})
	require.Error(t, err)
}
