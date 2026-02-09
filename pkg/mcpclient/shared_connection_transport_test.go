package mcpclient

import (
	"context"
	"errors"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

var errTestConnect = errors.New("connect failed")

func TestNewSharedConnectionTransportReusesConnection(t *testing.T) {
	t.Parallel()

	base := &countingTransport{
		connectFn: func() (mcp.Connection, error) {
			return &fakeSharedConn{}, nil
		},
	}

	shared := NewSharedConnectionTransport(base)
	require.NotNil(t, shared)

	connA, err := shared.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, connA)

	connB, err := shared.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, connB)

	require.Same(t, connA, connB)
	require.Equal(t, 1, base.connectCalls)
}

func TestNewSharedConnectionTransportNilBase(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewSharedConnectionTransport(nil))
}

func TestNewSharedConnectionTransportRetriesAfterFailure(t *testing.T) {
	t.Parallel()

	var base *countingTransport
	base = &countingTransport{
		connectFn: func() (mcp.Connection, error) {
			if base.connectCalls == 0 {
				return nil, errTestConnect
			}
			return &fakeSharedConn{}, nil
		},
	}

	shared := NewSharedConnectionTransport(base)
	require.NotNil(t, shared)

	conn, err := shared.Connect(context.Background())
	require.ErrorIs(t, err, errTestConnect)
	require.Nil(t, conn)

	conn, err = shared.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.Equal(t, 2, base.connectCalls)
}

func TestNewSharedConnectionTransportReconnectsAfterClose(t *testing.T) {
	t.Parallel()

	closer := &countingTransport{
		connectFn: func() (mcp.Connection, error) {
			return &closeTrackingConn{}, nil
		},
	}

	shared := NewSharedConnectionTransport(closer)
	require.NotNil(t, shared)

	conn, err := shared.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conn)

	require.NoError(t, conn.Close())

	conn2, err := shared.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conn2)
	require.NotSame(t, conn, conn2)
	require.Equal(t, 2, closer.connectCalls)
}

type countingTransport struct {
	connectCalls int
	connectFn    func() (mcp.Connection, error)
}

func (t *countingTransport) Connect(context.Context) (mcp.Connection, error) {
	conn, err := t.connectFn()
	t.connectCalls++
	return conn, err
}

type fakeSharedConn struct{}

func (fakeSharedConn) Read(context.Context) (jsonrpc.Message, error) { return nil, nil }
func (fakeSharedConn) Write(context.Context, jsonrpc.Message) error  { return nil }
func (fakeSharedConn) Close() error                                  { return nil }
func (fakeSharedConn) SessionID() string                             { return "" }

type closeTrackingConn struct {
	closed int
}

func (c *closeTrackingConn) Read(context.Context) (jsonrpc.Message, error) { return nil, nil }
func (c *closeTrackingConn) Write(context.Context, jsonrpc.Message) error  { return nil }
func (c *closeTrackingConn) Close() error                                  { c.closed++; return nil }
func (c *closeTrackingConn) SessionID() string                             { return "" }
