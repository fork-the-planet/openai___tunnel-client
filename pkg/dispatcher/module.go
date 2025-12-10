// Package dispatcher owns the bounded in-memory queue that decouples pollers
// from MCP workers.
package dispatcher

import (
	"context"
	"fmt"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.uber.org/fx"

	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/controlplane"
	dispatcherinternal "go.openai.org/api/tunnel-client/pkg/dispatcher/internal"
)

// Params captures the dependencies needed to size the dispatcher work queue.
type Params struct {
	fx.In

	ControlPlane  *config.ControlPlaneConfig
	MeterProvider *sdkmetric.MeterProvider
}

// Result exposes the bounded queue that downstream components consume.
type Result struct {
	fx.Out

	PolledCommandQueue controlplane.PolledCommandQueue
}

func newPolledCommandQueue(p Params) Result {
	size := 1
	if p.ControlPlane != nil && p.ControlPlane.MaxInFlightRequests > 0 {
		size = p.ControlPlane.MaxInFlightRequests
	}

	return Result{
		PolledCommandQueue: make(controlplane.PolledCommandQueue, size),
	}
}

// Module registers the dispatcher components with the Fx graph. It provides the
// bounded polled command queue sized according to ControlPlaneConfig, constructs
// the Processor that consumes commands from that queue and calls downstream MCP servers, and starts the listener
// goroutine that drains the queue when the app lifecycle begins.
var Module = fx.Module(
	"dispatcher",
	fx.Provide(
		newPolledCommandQueue,
		dispatcherinternal.NewProcessor,
		dispatcherinternal.NewQueueListener,
	),
	fx.Invoke(startQueueListener),
)

type listenerParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Listener  *dispatcherinternal.QueueListener
}

func startQueueListener(p listenerParams) error {
	if p.Listener == nil {
		return fmt.Errorf("dispatcher: queue listener is nil")
	}

	ctx, cancel := context.WithCancel(context.Background())

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(context.Context) error {
			p.Listener.Start(ctx)
			return nil
		},
		OnStop: func(context.Context) error {
			cancel()
			p.Listener.Wait()
			return nil
		},
	})

	return nil
}
