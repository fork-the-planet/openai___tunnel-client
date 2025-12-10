package dispatcherinternal

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/panjf2000/ants/v2"

	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/controlplane"
	tclog "go.openai.org/api/tunnel-client/pkg/log"
	"go.openai.org/api/tunnel-client/pkg/tunnelctx"
)

const poolReleaseTimeout = 10 * time.Second

// QueueListener drains the polled command queue and forwards work to the processor
// using a bounded worker pool.
type QueueListener struct {
	logger    *slog.Logger
	processor Processor
	queue     controlplane.PolledCommandQueue
	pool      *ants.Pool

	listenerWG sync.WaitGroup
}

// NewQueueListener constructs a QueueListener with a worker pool sized according to the MCP configuration.
func NewQueueListener(logger *slog.Logger, processor Processor, queue controlplane.PolledCommandQueue, mcpConfig *config.MCPConfig) (*QueueListener, error) {
	if logger == nil {
		return nil, fmt.Errorf("dispatcher queue listener: nil logger")
	}
	if processor == nil {
		return nil, fmt.Errorf("dispatcher queue listener: nil processor")
	}
	if queue == nil {
		return nil, fmt.Errorf("dispatcher queue listener: nil queue")
	}
	if mcpConfig == nil {
		return nil, fmt.Errorf("dispatcher queue listener: nil MCP config")
	}
	if mcpConfig.MaxConcurrentRequests <= 0 {
		return nil, fmt.Errorf("dispatcher queue listener: non-positive max concurrent requests")
	}

	pool, err := ants.NewPool(mcpConfig.MaxConcurrentRequests)
	if err != nil {
		return nil, fmt.Errorf("dispatcher queue listener: create worker pool: %w", err)
	}

	baseLogger := logger.With(tclog.FieldComponent, tclog.ComponentDispatcher)

	return &QueueListener{
		logger:    baseLogger,
		processor: processor,
		queue:     queue,
		pool:      pool,
	}, nil
}

// Start begins draining the queue until the provided context is canceled or the queue is closed.
func (l *QueueListener) Start(ctx context.Context) {
	l.listenerWG.Add(1)
	go func() {
		defer l.listenerWG.Done()
		l.run(ctx)
	}()
}

// Wait blocks until the listener has stopped processing commands.
func (l *QueueListener) Wait() {
	l.listenerWG.Wait()
}

func (l *QueueListener) run(ctx context.Context) {
	defer func() {
		if err := l.pool.ReleaseTimeout(poolReleaseTimeout); err != nil {
			l.logger.WarnContext(ctx, "failed to release dispatcher worker pool",
				slog.String("error", err.Error()))
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case cmd, ok := <-l.queue:
			if !ok {
				return
			}

			requestID := cmd.RequestID().String()
			cmdCopy := cmd
			cmdCtx := tunnelctx.ContextWithRequestID(ctx, requestID)
			if sessionID, ok := cmd.SessionID(); ok {
				cmdCtx = tunnelctx.ContextWithSessionID(cmdCtx, sessionID)
			}
			cmdLogger := tclog.LoggerWithContextIdentifiers(cmdCtx, l.logger)

			if err := l.pool.Submit(func() {
				if err := l.processor.Process(cmdCtx, cmdCopy); err != nil {
					cmdLogger.WarnContext(cmdCtx, "failed to process polled command",
						slog.String("error", err.Error()))
				}
			}); err != nil {
				cmdLogger.ErrorContext(cmdCtx, "failed to submit polled command to worker pool",
					slog.String("error", err.Error()))
				return
			}
		}
	}
}
