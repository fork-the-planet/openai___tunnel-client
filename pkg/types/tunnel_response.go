package types

import (
	"errors"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// ResponseType enumerates the kinds of responses that can be returned to the
// control plane.
type ResponseType int

const (
	// ResponseTypeJSONRPCResponse indicates the payload carries a JSON-RPC
	// response from the MCP server.
	ResponseTypeJSONRPCResponse ResponseType = iota
	// ResponseTypeNotificationAcknowledgment indicates the payload acknowledges a
	// notification that produced no JSON-RPC response body.
	ResponseTypeNotificationAcknowledgment
)

// TunnelResponse bundles the MCP response metadata (status code + headers) with
// either a JSON-RPC response message or a notification acknowledgement.
type TunnelResponse struct {
	response     *jsonrpc.Response
	headers      http.Header
	responseCode int
	responseType ResponseType
}

// NewTunnelResponse constructs a TunnelResponse, defensively copying the
// provided headers map so callers can mutate their copy without affecting the
// payload delivered to tunnel-service.
func NewTunnelResponse(response *jsonrpc.Response, code int, headers http.Header) *TunnelResponse {
	return &TunnelResponse{
		response:     response,
		headers:      cloneHeaders(headers),
		responseCode: code,
		responseType: ResponseTypeJSONRPCResponse,
	}
}

// NewNotificationAck constructs a TunnelResponse representing a successful
// acknowledgement of a JSON-RPC notification (which carries no response body).
func NewNotificationAck(code int, headers http.Header) *TunnelResponse {
	return &TunnelResponse{
		headers:      cloneHeaders(headers),
		responseCode: code,
		responseType: ResponseTypeNotificationAcknowledgment,
	}
}

// JSONRPC returns the underlying JSON-RPC response message.
func (t *TunnelResponse) JSONRPC() *jsonrpc.Response {
	if t == nil {
		return nil
	}
	return t.response
}

// Type returns the response type enum.
func (t *TunnelResponse) Type() ResponseType {
	if t == nil {
		return ResponseTypeJSONRPCResponse
	}
	return t.responseType
}

// ResponseCode returns the HTTP status code observed when forwarding the
// request to the MCP server.
func (t *TunnelResponse) ResponseCode() int {
	if t == nil {
		return 0
	}
	return t.responseCode
}

// Headers returns a defensive copy of the response headers map.
func (t *TunnelResponse) Headers() http.Header {
	if t == nil || t.headers == nil {
		return nil
	}
	return t.headers.Clone()
}

// Validate returns an error if the response is structurally invalid.
func (t *TunnelResponse) Validate() error {
	if t == nil {
		return errors.New("tunnel response is nil")
	}
	if t.responseType == ResponseTypeNotificationAcknowledgment && t.response != nil {
		return errors.New("notification acknowledgments must not include a jsonrpc response")
	}
	if t.responseType != ResponseTypeNotificationAcknowledgment && t.response == nil {
		return errors.New("jsonrpc response is required")
	}
	return nil
}

func cloneHeaders(headers http.Header) http.Header {
	if headers == nil {
		return nil
	}
	return headers.Clone()
}
