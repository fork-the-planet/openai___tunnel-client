package mcpclient

import (
	"context"
	"net/http"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
)

// NewSerializedForwardingTransport wraps a ForwardingTransport so only one
// request lifecycle is active on the shared underlying connection at a time.
//
// Some MCP transports multiplex poorly when several connector calls write to the
// same long-lived connection and then each reader waits for its own response. The
// wrapper holds a lifecycle lock from Write through any streamed notifications
// until the matching final JSON-RPC response, an error, or Close. Notifications
// without ids release immediately after the write because no response is legal.
func NewSerializedForwardingTransport(base ForwardingTransport) ForwardingTransport {
	if base == nil {
		return nil
	}
	return &serializedForwardingTransport{base: base}
}

type serializedForwardingTransport struct {
	base ForwardingTransport
	mu   sync.Mutex
}

func (t *serializedForwardingTransport) Connect(
	ctx context.Context,
) (ForwardingConnection, error) {
	conn, err := t.base.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &serializedForwardingConnection{
		base:        conn,
		lifecycleMu: &t.mu,
	}, nil
}

type serializedForwardingConnection struct {
	base ForwardingConnection

	lifecycleMu *sync.Mutex

	stateMu          sync.Mutex
	lockHeld         bool
	awaitingResponse bool
	expectedID       jsonrpc.ID
}

func (c *serializedForwardingConnection) Write(
	ctx context.Context,
	header http.Header,
	msg jsonrpc.Message,
) (int, http.Header, error) {
	if c.base == nil {
		return 0, nil, nil
	}

	expectResponse, expectedID := expectedResponseID(msg)
	c.acquire(expectResponse, expectedID)

	statusCode, respHeaders, err := c.base.Write(ctx, header, msg)
	if err != nil || !expectResponse || statusCode >= http.StatusBadRequest {
		c.release()
	}
	return statusCode, respHeaders, err
}

func (c *serializedForwardingConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if c.base == nil {
		return nil, nil
	}

	msg, err := c.base.Read(ctx)
	if err != nil || msg == nil {
		c.release()
		return msg, err
	}
	if c.shouldReleaseAfterRead(msg) {
		c.release()
	}
	return msg, err
}

func (c *serializedForwardingConnection) Close() error {
	if c.base == nil {
		c.release()
		return nil
	}
	defer c.release()
	return c.base.Close()
}

func (c *serializedForwardingConnection) acquire(expectResponse bool, expectedID jsonrpc.ID) {
	if c.lifecycleMu != nil {
		c.lifecycleMu.Lock()
	}
	c.stateMu.Lock()
	c.lockHeld = true
	c.awaitingResponse = expectResponse
	c.expectedID = expectedID
	c.stateMu.Unlock()
}

func (c *serializedForwardingConnection) shouldReleaseAfterRead(msg jsonrpc.Message) bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()

	if !c.lockHeld {
		return false
	}
	if !c.awaitingResponse {
		return true
	}

	response, ok := msg.(*jsonrpc.Response)
	if !ok {
		return false
	}

	if !response.ID.IsValid() || response.ID != c.expectedID {
		c.awaitingResponse = false
		return true
	}

	c.awaitingResponse = false
	return true
}

func (c *serializedForwardingConnection) release() {
	c.stateMu.Lock()
	lockHeld := c.lockHeld
	c.lockHeld = false
	c.awaitingResponse = false
	c.expectedID = jsonrpc.ID{}
	c.stateMu.Unlock()

	if lockHeld && c.lifecycleMu != nil {
		c.lifecycleMu.Unlock()
	}
}

func expectedResponseID(msg jsonrpc.Message) (bool, jsonrpc.ID) {
	request, ok := msg.(*jsonrpc.Request)
	if !ok || !request.ID.IsValid() {
		return false, jsonrpc.ID{}
	}
	return true, request.ID
}
