package mcpclient

import (
	"context"
	"net/http"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/openai/tunnel-client/pkg/mcpclient/internal"
)

type responseDeadlineEnforcementContextKey struct{}

// ContextWithResponseDeadlineEnforcement marks MCP work whose full lifecycle
// is bounded by a tunnel command response deadline. Legacy commands without
// response_timeout intentionally remain unmarked.
func ContextWithResponseDeadlineEnforcement(ctx context.Context) context.Context {
	if ctx == nil {
		return nil
	}
	return context.WithValue(ctx, responseDeadlineEnforcementContextKey{}, true)
}

func hasResponseDeadlineEnforcement(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	marked, _ := ctx.Value(responseDeadlineEnforcementContextKey{}).(bool)
	return marked
}

var _ ForwardingTransport = (*forwardingTransport)(nil)
var _ SessionTerminatingTransport = (*forwardingTransport)(nil)

// forwardingTransport bridges the public ForwardingTransport interface to the
// internal implementation.
type forwardingTransport struct {
	base mcp.Transport
}

func (t *forwardingTransport) Connect(ctx context.Context) (ForwardingConnection, error) {
	if t == nil || t.base == nil {
		return nil, nil
	}
	conn, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &forwardingConnection{
		base: conn,
	}, nil
}

func (t *forwardingTransport) TerminateSession(ctx context.Context, headers http.Header) (int, http.Header, error) {
	if t == nil || t.base == nil {
		return 0, nil, nil
	}
	ctxWithHeaders, carrier, err := internal.ContextWithHeaders(ctx, headers)
	if err != nil {
		return 0, nil, err
	}
	if streamable, ok := unwrapStreamableClientTransport(t.base); ok && hasResponseDeadlineEnforcement(ctx) {
		return terminateStreamableSession(ctxWithHeaders, streamable, headers)
	}

	conn, err := t.base.Connect(ctxWithHeaders)
	if err != nil {
		return 0, nil, err
	}
	err = conn.Close()
	statusCode, responseHeaders := carrier.ResponseStatusAndHeaders()
	return statusCode, responseHeaders, err
}

func unwrapStreamableClientTransport(transport mcp.Transport) (*mcp.StreamableClientTransport, bool) {
	switch typed := transport.(type) {
	case *mcp.StreamableClientTransport:
		return typed, typed != nil
	case *mcp.LoggingTransport:
		if typed == nil {
			return nil, false
		}
		return unwrapStreamableClientTransport(typed.Transport)
	default:
		return nil, false
	}
}

// terminateStreamableSession issues the protocol DELETE directly so the
// command context remains attached to the network request. The SDK connection
// Close method uses a detached lifecycle context, which cannot enforce a
// per-command response deadline.
func terminateStreamableSession(ctx context.Context, transport *mcp.StreamableClientTransport, headers http.Header) (int, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, transport.Endpoint, nil)
	if err != nil {
		return 0, nil, err
	}
	if headers != nil {
		req.Header = headers.Clone()
	}

	client := transport.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		if resp.Body != nil {
			_ = resp.Body.Close()
		}
	}()
	return resp.StatusCode, resp.Header.Clone(), nil
}

var _ ForwardingConnection = (*forwardingConnection)(nil)

// forwardingConnection delegates all behavior to the internal connection
// implementation while satisfying the public ForwardingConnection interface.
type forwardingConnection struct {
	base mcp.Connection
}

func (c *forwardingConnection) Close() error {
	if c.base == nil {
		return nil
	}
	return c.base.Close()
}

func (c *forwardingConnection) Write(ctx context.Context, header http.Header, msg jsonrpc.Message) (int, http.Header, error) {
	if c.base == nil {
		return 0, nil, nil
	}
	ctxWithHeaders, carrier, err := internal.ContextWithHeaders(ctx, header)
	if err != nil {
		return 0, nil, err
	}

	err = c.base.Write(ctxWithHeaders, msg)
	var (
		respHeaders http.Header
		statusCode  int
	)
	if carrier != nil {
		statusCode, respHeaders = carrier.ResponseStatusAndHeaders()
	}

	if err != nil {
		_ = c.base.Close()
	}

	return statusCode, respHeaders, err
}

func (c *forwardingConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if c.base == nil {
		return nil, nil
	}
	msg, err := c.base.Read(ctx)
	if err != nil {
		_ = c.base.Close()
	}
	return msg, err
}
