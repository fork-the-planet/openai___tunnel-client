package mcpclient

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"
)

func TestNewSerializedForwardingTransportNilBaseReturnsNil(t *testing.T) {
	t.Parallel()

	require.Nil(t, NewSerializedForwardingTransport(nil))
}

func TestSerializedForwardingTransportHoldsLifecycleLockUntilMatchingResponse(t *testing.T) {
	t.Parallel()

	baseConn := newStubSerializedForwardingConnection()
	transport := NewSerializedForwardingTransport(&stubSerializedForwardingTransport{
		conn: baseConn,
	})
	require.NotNil(t, transport)
	serializedTransport := transport.(*serializedForwardingTransport)

	connA, err := transport.Connect(context.Background())
	require.NoError(t, err)
	idA, err := jsonrpc.MakeID("a")
	require.NoError(t, err)
	reqA := &jsonrpc.Request{ID: idA, Method: "tools/call"}
	_, _, err = connA.Write(context.Background(), nil, reqA)
	require.NoError(t, err)
	requireLifecycleLockHeld(t, serializedTransport)

	notification := &jsonrpc.Request{Method: "notifications/progress"}
	baseConn.enqueueRead(notification, nil)
	msg, err := connA.Read(context.Background())
	require.NoError(t, err)
	require.Same(t, notification, msg)
	requireLifecycleLockHeld(t, serializedTransport)

	responseA := &jsonrpc.Response{ID: idA}
	baseConn.enqueueRead(responseA, nil)
	msg, err = connA.Read(context.Background())
	require.NoError(t, err)
	require.Same(t, responseA, msg)
	requireLifecycleLockReleased(t, serializedTransport)

	idB, err := jsonrpc.MakeID("b")
	require.NoError(t, err)
	reqB := &jsonrpc.Request{ID: idB, Method: "tools/call"}
	_, _, err = connA.Write(context.Background(), nil, reqB)
	require.NoError(t, err)
	requireLifecycleLockHeld(t, serializedTransport)

	require.NoError(t, connA.Close())
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
	idA, err := jsonrpc.MakeID("a")
	require.NoError(t, err)
	reqA := &jsonrpc.Request{ID: idA, Method: "tools/call"}
	status, _, err := connA.Write(context.Background(), nil, reqA)
	require.NoError(t, err)
	require.Equal(t, http.StatusBadGateway, status)

	connB, err := transport.Connect(context.Background())
	require.NoError(t, err)
	idB, err := jsonrpc.MakeID("b")
	require.NoError(t, err)
	reqB := &jsonrpc.Request{ID: idB, Method: "tools/call"}
	status, _, err = connB.Write(context.Background(), nil, reqB)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)

	require.NoError(t, connB.Close())
}

func TestSerializedForwardingTransportCanceledWaiterDoesNotConnectBase(t *testing.T) {
	t.Parallel()

	baseConn := newStubSerializedForwardingConnection()
	baseTransport := &stubSerializedForwardingTransport{conn: baseConn}
	transport := NewSerializedForwardingTransport(baseTransport)

	first, err := transport.Connect(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 1, baseTransport.connectCalls.Load())

	waitCtx, cancelWait := context.WithCancel(context.Background())
	doneObserved := make(chan struct{})
	waitCtx = &doneObservedContext{Context: waitCtx, observed: doneObserved}
	result := make(chan serializedConnectResult, 1)
	go func() {
		conn, connectErr := transport.Connect(waitCtx)
		result <- serializedConnectResult{conn: conn, err: connectErr}
	}()

	waitForSerializedSignal(t, doneObserved, "second Connect to wait for the lifecycle slot")
	cancelWait()
	got := waitForSerializedConnectResult(t, result)
	require.Nil(t, got.conn)
	require.ErrorIs(t, got.err, context.Canceled)
	require.EqualValues(t, 1, baseTransport.connectCalls.Load(), "canceled waiter must not call base.Connect")

	require.NoError(t, first.Close())
	third, err := transport.Connect(context.Background())
	require.NoError(t, err)
	require.EqualValues(t, 2, baseTransport.connectCalls.Load(), "released slot should admit the next lifecycle")
	require.NoError(t, third.Close())
}

func TestSerializedForwardingTransportReleasesAfterBaseConnectError(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("connect failed")
	baseTransport := &failOnceSerializedForwardingTransport{
		conn: newStubSerializedForwardingConnection(),
		err:  baseErr,
	}
	transport := NewSerializedForwardingTransport(baseTransport)

	conn, err := transport.Connect(context.Background())
	require.Nil(t, conn)
	require.ErrorIs(t, err, baseErr)

	conn, err = transport.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, conn)
	require.NoError(t, conn.Close())
	require.EqualValues(t, 2, baseTransport.connectCalls.Load())
}

func TestSerializedForwardingTransportClosesAndReleasesWhenContextExpiresDuringBaseConnect(t *testing.T) {
	t.Parallel()

	firstConn := newStubSerializedForwardingConnection()
	secondConn := newStubSerializedForwardingConnection()
	baseTransport := &blockingFirstSerializedForwardingTransport{
		firstConn:  firstConn,
		secondConn: secondConn,
		started:    make(chan struct{}),
		release:    make(chan struct{}),
	}
	transport := NewSerializedForwardingTransport(baseTransport)
	ctx, cancel := context.WithCancel(context.Background())
	result := make(chan serializedConnectResult, 1)
	go func() {
		conn, connectErr := transport.Connect(ctx)
		result <- serializedConnectResult{conn: conn, err: connectErr}
	}()

	waitForSerializedSignal(t, baseTransport.started, "base Connect to start")
	cancel()
	close(baseTransport.release)
	got := waitForSerializedConnectResult(t, result)
	require.Nil(t, got.conn)
	require.ErrorIs(t, got.err, context.Canceled)
	require.EqualValues(t, 1, firstConn.closeCalls.Load(), "connection returned after cancellation must be closed")

	next, err := transport.Connect(context.Background())
	require.NoError(t, err)
	require.NotNil(t, next)
	require.EqualValues(t, 2, baseTransport.connectCalls.Load())
	require.NoError(t, next.Close())
}

func requireLifecycleLockHeld(t *testing.T, transport *serializedForwardingTransport) {
	t.Helper()
	require.Equal(t, 1, len(transport.lifecycleSlot), "lifecycle slot was released")
}

func requireLifecycleLockReleased(t *testing.T, transport *serializedForwardingTransport) {
	t.Helper()
	require.Empty(t, transport.lifecycleSlot, "lifecycle slot was still held")
}

type stubSerializedForwardingTransport struct {
	conn         ForwardingConnection
	connectCalls atomic.Int32
}

func (s *stubSerializedForwardingTransport) Connect(context.Context) (ForwardingConnection, error) {
	s.connectCalls.Add(1)
	return s.conn, nil
}

type failOnceSerializedForwardingTransport struct {
	conn         ForwardingConnection
	err          error
	connectCalls atomic.Int32
}

type blockingFirstSerializedForwardingTransport struct {
	firstConn    ForwardingConnection
	secondConn   ForwardingConnection
	started      chan struct{}
	release      chan struct{}
	connectCalls atomic.Int32
}

func (s *blockingFirstSerializedForwardingTransport) Connect(context.Context) (ForwardingConnection, error) {
	if s.connectCalls.Add(1) == 1 {
		close(s.started)
		<-s.release
		return s.firstConn, nil
	}
	return s.secondConn, nil
}

func (s *failOnceSerializedForwardingTransport) Connect(context.Context) (ForwardingConnection, error) {
	if s.connectCalls.Add(1) == 1 {
		return nil, s.err
	}
	return s.conn, nil
}

type doneObservedContext struct {
	context.Context
	observed chan struct{}
	once     sync.Once
}

func (c *doneObservedContext) Done() <-chan struct{} {
	c.once.Do(func() { close(c.observed) })
	return c.Context.Done()
}

type serializedConnectResult struct {
	conn ForwardingConnection
	err  error
}

func waitForSerializedSignal(t *testing.T, signal <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", description)
	}
}

func waitForSerializedConnectResult(t *testing.T, result <-chan serializedConnectResult) serializedConnectResult {
	t.Helper()
	select {
	case got := <-result:
		return got
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for serialized Connect result")
		return serializedConnectResult{}
	}
}

type stubSerializedForwardingConnection struct {
	writeResults chan stubWriteResult
	readResults  chan stubReadResult
	closeCalls   atomic.Int32
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
	s.closeCalls.Add(1)
	return nil
}
