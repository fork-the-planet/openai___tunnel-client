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
// wrapper holds a lifecycle slot from Connect through any streamed notifications
// until the matching final JSON-RPC response, an error, or Close. Notifications
// without ids release immediately after the write because no response is legal.
func NewSerializedForwardingTransport(base ForwardingTransport) ForwardingTransport {
	if base == nil {
		return nil
	}
	return &serializedForwardingTransport{
		base:          base,
		lifecycleSlot: make(chan struct{}, 1),
	}
}

type serializedForwardingTransport struct {
	base          ForwardingTransport
	lifecycleSlot chan struct{}
}

func (t *serializedForwardingTransport) Connect(
	ctx context.Context,
) (ForwardingConnection, error) {
	if err := t.acquireLifecycle(ctx); err != nil {
		return nil, err
	}

	conn, err := t.base.Connect(ctx)
	if err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		t.releaseLifecycle()
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		if conn != nil {
			_ = conn.Close()
		}
		t.releaseLifecycle()
		return nil, err
	}
	return &serializedForwardingConnection{
		acquireLifecycle: t.acquireLifecycle,
		base:             conn,
		releaseLifecycle: t.releaseLifecycle,
		lockHeld:         true,
	}, nil
}

func (t *serializedForwardingTransport) acquireLifecycle(ctx context.Context) error {
	select {
	case t.lifecycleSlot <- struct{}{}:
		if err := ctx.Err(); err != nil {
			t.releaseLifecycle()
			return err
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (t *serializedForwardingTransport) releaseLifecycle() {
	<-t.lifecycleSlot
}

type serializedForwardingConnection struct {
	base ForwardingConnection

	acquireLifecycle func(context.Context) error
	releaseLifecycle func()

	stateMu          sync.Mutex
	lockHeld         bool
	writeStarted     bool
	awaitingResponse bool
	expectedID       jsonrpc.ID
}

func (c *serializedForwardingConnection) Write(
	ctx context.Context,
	header http.Header,
	msg jsonrpc.Message,
) (int, http.Header, error) {
	if c.base == nil {
		c.release()
		return 0, nil, nil
	}

	expectResponse, expectedID := expectedResponseID(msg)
	if err := c.acquire(ctx, expectResponse, expectedID); err != nil {
		return 0, nil, err
	}

	statusCode, respHeaders, err := c.base.Write(ctx, header, msg)
	if err != nil || !expectResponse || statusCode >= http.StatusBadRequest {
		c.release()
	}
	return statusCode, respHeaders, err
}

func (c *serializedForwardingConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if c.base == nil {
		c.release()
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

func (c *serializedForwardingConnection) acquire(ctx context.Context, expectResponse bool, expectedID jsonrpc.ID) error {
	c.stateMu.Lock()
	if c.lockHeld && !c.writeStarted {
		c.writeStarted = true
		c.awaitingResponse = expectResponse
		c.expectedID = expectedID
		c.stateMu.Unlock()
		return nil
	}
	c.stateMu.Unlock()

	if c.acquireLifecycle != nil {
		if err := c.acquireLifecycle(ctx); err != nil {
			return err
		}
	}

	c.stateMu.Lock()
	c.lockHeld = true
	c.writeStarted = true
	c.awaitingResponse = expectResponse
	c.expectedID = expectedID
	c.stateMu.Unlock()
	return nil
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
	if !c.markReleased() {
		return
	}
	if c.releaseLifecycle != nil {
		c.releaseLifecycle()
	}
}

func (c *serializedForwardingConnection) markReleased() bool {
	c.stateMu.Lock()
	lockHeld := c.lockHeld
	c.lockHeld = false
	c.writeStarted = false
	c.awaitingResponse = false
	c.expectedID = jsonrpc.ID{}
	c.stateMu.Unlock()
	return lockHeld
}

func expectedResponseID(msg jsonrpc.Message) (bool, jsonrpc.ID) {
	request, ok := msg.(*jsonrpc.Request)
	if !ok || !request.ID.IsValid() {
		return false, jsonrpc.ID{}
	}
	return true, request.ID
}
