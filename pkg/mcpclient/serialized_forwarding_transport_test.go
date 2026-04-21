package mcpclient

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestNewSerializedForwardingTransportNilBaseReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewSerializedForwardingTransport(nil))
}

func TestSerializedForwardingTransportBlocksSecondWriteUntilMatchingResponse(t *testing.T) {
	t.Parallel()

	baseConn := newStubSerializedForwardingConnection()
	transport := NewSerializedForwardingTransport(&stubSerializedForwardingTransport{
		conn: baseConn,
	})
	require.NotNil(t, transport)

	connA, err := transport.Connect(context.Background())
	require.NoError(t, err)
	connB, err := transport.Connect(context.Background())
	require.NoError(t, err)

	idA, err := jsonrpc.MakeID("a")
	require.NoError(t, err)
	reqA := &jsonrpc.Request{ID: idA, Method: "tools/call"}
	_, _, err = connA.Write(context.Background(), nil, reqA)
	require.NoError(t, err)

	idB, err := jsonrpc.MakeID("b")
	require.NoError(t, err)
	reqB := &jsonrpc.Request{ID: idB, Method: "tools/call"}

	secondWriteDone := make(chan error, 1)
	go func() {
		_, _, writeErr := connB.Write(context.Background(), nil, reqB)
		secondWriteDone <- writeErr
	}()

	select {
	case err := <-secondWriteDone:
		t.Fatalf("second Write returned before first request finished: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	notification := &jsonrpc.Request{Method: "notifications/progress"}
	baseConn.enqueueRead(notification, nil)
	msg, err := connA.Read(context.Background())
	require.NoError(t, err)
	require.Same(t, notification, msg)

	select {
	case err := <-secondWriteDone:
		t.Fatalf("second Write returned after notification instead of final response: %v", err)
	case <-time.After(50 * time.Millisecond):
	}

	responseA := &jsonrpc.Response{ID: idA}
	baseConn.enqueueRead(responseA, nil)
	msg, err = connA.Read(context.Background())
	require.NoError(t, err)
	require.Same(t, responseA, msg)

	select {
	case err := <-secondWriteDone:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("second Write did not resume after first response was read")
	}

	require.NoError(t, connB.Close())
}

func TestSerializedForwardingTransportReleasesAfterUpstreamErrorStatus(t *testing.T) {
	t.Parallel()

	baseConn := newStubSerializedForwardingConnection()
	baseConn.enqueueWriteResult(http.StatusBadGateway, nil, nil)
	baseConn.enqueueWriteResult(http.StatusOK, nil, nil)

	transport := NewSerializedForwardingTransport(&stubSerializedForwardingTransport{
		conn: baseConn,
	})
	require.NotNil(t, transport)

	connA, err := transport.Connect(context.Background())
	require.NoError(t, err)
	connB, err := transport.Connect(context.Background())
	require.NoError(t, err)

	idA, err := jsonrpc.MakeID("a")
	require.NoError(t, err)
	reqA := &jsonrpc.Request{ID: idA, Method: "tools/call"}
	status, _, err := connA.Write(context.Background(), nil, reqA)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, status)

	idB, err := jsonrpc.MakeID("b")
	require.NoError(t, err)
	reqB := &jsonrpc.Request{ID: idB, Method: "tools/call"}
	status, _, err = connB.Write(context.Background(), nil, reqB)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	require.NoError(t, connB.Close())
}

type stubSerializedForwardingTransport struct {
	conn ForwardingConnection
}

func (s *stubSerializedForwardingTransport) Connect(context.Context) (ForwardingConnection, error) {
	return s.conn, nil
}

type stubSerializedForwardingConnection struct {
	writeResults chan stubWriteResult
	readResults  chan stubReadResult
}

type stubWriteResult struct {
	statusCode int
	headers    http.Header
	err        error
}

type stubReadResult struct {
	msg jsonrpc.Message
	err error
}

func newStubSerializedForwardingConnection() *stubSerializedForwardingConnection {
	return &stubSerializedForwardingConnection{
		writeResults: make(chan stubWriteResult, 8),
		readResults:  make(chan stubReadResult, 8),
	}
}

func (s *stubSerializedForwardingConnection) enqueueWriteResult(
	statusCode int,
	headers http.Header,
	err error,
) {
	s.writeResults <- stubWriteResult{
		statusCode: statusCode,
		headers:    headers,
		err:        err,
	}
}

func (s *stubSerializedForwardingConnection) enqueueRead(msg jsonrpc.Message, err error) {
	s.readResults <- stubReadResult{msg: msg, err: err}
}

func (s *stubSerializedForwardingConnection) Write(
	context.Context,
	http.Header,
	jsonrpc.Message,
) (int, http.Header, error) {
	select {
	case result := <-s.writeResults:
		return result.statusCode, result.headers, result.err
	default:
		return http.StatusOK, nil, nil
	}
}

func (s *stubSerializedForwardingConnection) Read(context.Context) (jsonrpc.Message, error) {
	result := <-s.readResults
	return result.msg, result.err
}

func (s *stubSerializedForwardingConnection) Close() error {
	return nil
}
