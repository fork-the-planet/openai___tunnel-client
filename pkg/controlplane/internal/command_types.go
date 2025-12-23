package internal

import (
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"go.openai.org/api/tunnel-client/pkg/types"
)

// basePolledCommand contains fields common to all polled command implementations
// and implements the internal.PolledCommand interface.
type basePolledCommand struct {
	requestID  types.RequestID
	enqueued   time.Time
	polledAt   time.Time
	headers    http.Header
	sessionID  *string
	shardToken string
}

func (c *basePolledCommand) RequestID() types.RequestID { return c.requestID }
func (c *basePolledCommand) EnqueuedAt() time.Time      { return c.enqueued }
func (c *basePolledCommand) PolledAt() time.Time        { return c.polledAt }
func (c *basePolledCommand) Headers() http.Header {
	if c.headers == nil {
		return nil
	}
	return c.headers
}
func (c *basePolledCommand) ShardToken() string { return c.shardToken }
func (c *basePolledCommand) SessionID() (string, bool) {
	if c.sessionID == nil {
		return "", false
	}
	return *c.sessionID, true
}

// jsonRpcCommand represents a JSON-RPC command; it implements JsonRpcCommand via Message().
type jsonRpcCommand struct {
	basePolledCommand
	message jsonrpc.Message
}

// Message is only implemented by jsonRpcCommand
func (c *jsonRpcCommand) Message() jsonrpc.Message { return c.message }

// oauthDiscoveryCommand represents a non-JSON-RPC command (OAuth discovery).
// It intentionally does NOT include a Message() method so it will not satisfy
// JsonRpcCommand, allowing the dispatcher to distinguish the type.
type oauthDiscoveryCommand struct {
	basePolledCommand
}
