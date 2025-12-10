package dispatcherinternal

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"github.com/stretchr/testify/require"

	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/controlplane"
	"go.openai.org/api/tunnel-client/pkg/types"
)

func TestQueueListenerProcessesCommands(t *testing.T) {
	t.Parallel()

	const commandCount = 3

	processor := &stubProcessor{
		finished: make(chan types.RequestID, commandCount),
	}

	mcpConfig := &config.MCPConfig{
		ConnectionMaxTTL:      time.Second,
		MaxConcurrentRequests: 2,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	queue := make(controlplane.PolledCommandQueue, commandCount)
	listener, err := NewQueueListener(logger, processor, queue, mcpConfig)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener.Start(ctx)

	for i := 0; i < commandCount; i++ {
		queue <- newTestCommand(i)
	}
	close(queue)

	for i := 0; i < commandCount; i++ {
		select {
		case <-processor.finished:
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for command %d", i)
		}
	}

	listener.Wait()

	processor.requireCalls(t, commandCount)
}

func TestQueueListenerWaitBlocksUntilTasksComplete(t *testing.T) {
	t.Parallel()

	block := make(chan struct{})
	processor := &stubProcessor{
		started:  make(chan types.RequestID, 1),
		block:    block,
		finished: make(chan types.RequestID, 1),
	}

	mcpConfig := &config.MCPConfig{
		ConnectionMaxTTL:      time.Second,
		MaxConcurrentRequests: 1,
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	queue := make(controlplane.PolledCommandQueue, 1)
	listener, err := NewQueueListener(logger, processor, queue, mcpConfig)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener.Start(ctx)

	queue <- newTestCommand(0)
	close(queue)

	select {
	case <-processor.started:
	case <-time.After(time.Second):
		t.Fatal("processor never started")
	}

	waitDone := make(chan struct{})
	go func() {
		listener.Wait()
		close(waitDone)
	}()

	select {
	case <-waitDone:
		t.Fatal("listener.Wait returned before processor completed")
	case <-time.After(50 * time.Millisecond):
	}

	close(block)

	select {
	case <-waitDone:
	case <-time.After(time.Second):
		t.Fatal("listener.Wait did not finish after processor completed")
	}

	processor.requireCalls(t, 1)
}

type stubProcessor struct {
	mu       sync.Mutex
	calls    []types.RequestID
	started  chan types.RequestID
	finished chan types.RequestID
	block    chan struct{}
}

func (s *stubProcessor) Process(ctx context.Context, cmd controlplane.PolledCommand) error {
	if s.started != nil {
		select {
		case s.started <- cmd.RequestID():
		default:
		}
	}
	if s.block != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-s.block:
		}
	}

	s.mu.Lock()
	s.calls = append(s.calls, cmd.RequestID())
	s.mu.Unlock()

	if s.finished != nil {
		s.finished <- cmd.RequestID()
	}

	return nil
}

func (s *stubProcessor) requireCalls(t *testing.T, want int) {
	t.Helper()

	s.mu.Lock()
	defer s.mu.Unlock()

	require.Len(t, s.calls, want)
}

type queueTestCommand struct {
	id         types.RequestID
	message    jsonrpc.Message
	enqueuedAt time.Time
	polledAt   time.Time
	shardToken string
}

func newTestCommand(seq int) controlplane.PolledCommand {
	return &queueTestCommand{
		id:         types.RequestID("req-" + strconv.Itoa(seq)),
		message:    &jsonrpc.Request{Method: "exampleMethod"},
		enqueuedAt: time.Now(),
		polledAt:   time.Now(),
		shardToken: "shard-token-" + strconv.Itoa(seq),
	}
}

func (c *queueTestCommand) RequestID() types.RequestID {
	return c.id
}

func (c *queueTestCommand) Message() jsonrpc.Message {
	return c.message
}

func (c *queueTestCommand) EnqueuedAt() time.Time {
	return c.enqueuedAt
}

func (c *queueTestCommand) PolledAt() time.Time {
	return c.polledAt
}

func (c *queueTestCommand) Headers() http.Header {
	return nil
}

func (c *queueTestCommand) ShardToken() string {
	return c.shardToken
}

func (c *queueTestCommand) SessionID() (string, bool) {
	return "", false
}
