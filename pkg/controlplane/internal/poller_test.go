package internal

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"go.openai.org/api/tunnel-client/pkg/types"
)

type stubCommand struct {
	id types.RequestID
}

func (s stubCommand) RequestID() types.RequestID { return s.id }
func (s stubCommand) Message() jsonrpc.Message   { return &jsonrpc.Request{Method: "noop"} }
func (s stubCommand) EnqueuedAt() time.Time      { return time.Time{} }
func (s stubCommand) PolledAt() time.Time        { return time.Time{} }
func (s stubCommand) Headers() http.Header       { return nil }
func (s stubCommand) ShardToken() string         { return "" }
func (s stubCommand) SessionID() (string, bool) {
	return "", false
}

type agedCommand struct {
	stubCommand
	enqueuedAt time.Time
	polledAt   time.Time
}

func (c agedCommand) EnqueuedAt() time.Time { return c.enqueuedAt }
func (c agedCommand) PolledAt() time.Time   { return c.polledAt }

type recordingFetcher struct {
	t     *testing.T
	data  []PolledCommand
	mu    sync.Mutex
	calls []int
}

func (f *recordingFetcher) Poll(ctx context.Context, limit int) ([]PolledCommand, types.TunnelServiceRequestID, error) {
	if limit <= 0 {
		f.t.Fatalf("expected positive limit, got %d", limit)
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, limit)

	if len(f.data) == 0 {
		return nil, "", nil
	}
	n := limit
	if n > len(f.data) {
		n = len(f.data)
	}

	out := make([]PolledCommand, n)
	copy(out, f.data[:n])
	f.data = f.data[n:]
	return out, "", nil
}

func TestPollerWritesAtMostQueueCapacity(t *testing.T) {
	queue := make(chan PolledCommand, 2)
	queueAdapter := &chanQueue{ch: queue}
	fetcher := &recordingFetcher{
		t: t,
		data: []PolledCommand{
			stubCommand{id: "1"},
			stubCommand{id: "2"},
			stubCommand{id: "3"},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() {
		_ = meterProvider.Shutdown(context.Background())
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	poller, err := NewPoller(queueAdapter, fetcher, logger, meterProvider.Meter("test"), time.Second)
	if err != nil {
		t.Fatalf("new poller: %v", err)
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		poller.Run(ctx)
	}()

	received := make([]PolledCommand, 0, 3)

	waitForQueue := func() PolledCommand {
		select {
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for command")
			return nil
		case cmd := <-queue:
			return cmd
		}
	}

	for i := 0; i < 3; i++ {
		received = append(received, waitForQueue())
	}

	cancel()
	wg.Wait()

	if len(received) != 3 {
		t.Fatalf("expected 3 commands, got %d", len(received))
	}

	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()

	for _, limit := range fetcher.calls {
		if limit > cap(queue) {
			t.Fatalf("poll called with limit %d exceeding queue capacity %d", limit, cap(queue))
		}
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	assertCounterValue(t, rm, metricNameCommandsPolled, int64(len(received)))
	assertCounterValue(t, rm, metricNameCommandsEnqueued, int64(len(received)))
	assertGaugeValue(t, rm, metricNameQueueCapacity, int64(cap(queue)))
	assertGaugeValue(t, rm, metricNameQueueLength, int64(len(queue)))
}

func TestPollerRecordsQueueDropsAndCommandAge(t *testing.T) {
	queue := &failingQueue{}
	fetcher := &recordingFetcher{
		t: t,
		data: []PolledCommand{
			agedCommand{
				stubCommand: stubCommand{id: "1"},
				enqueuedAt:  time.Now().Add(-3 * time.Second),
				polledAt:    time.Now(),
			},
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() {
		_ = meterProvider.Shutdown(context.Background())
	}()

	poller, err := NewPoller(queue, fetcher, logger, meterProvider.Meter("test"), time.Second)
	if err != nil {
		t.Fatalf("new poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	poller.Run(ctx)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	assertCounterValue(t, rm, metricNameCommandsPolled, 1)
	assertCounterValueWithAttributes(t, rm, metricNameCommandsQueueDrops, attribute.String(attributeKeyDropReason, dropReasonQueueFull), 1)
	assertHistogramCount(t, rm, metricNameCommandsAge, 1)
}

type chanQueue struct {
	ch chan PolledCommand
}

func (q *chanQueue) Capacity() int {
	return cap(q.ch)
}

func (q *chanQueue) Length() int {
	return len(q.ch)
}

func (q *chanQueue) Enqueue(ctx context.Context, cmd PolledCommand) bool {
	select {
	case <-ctx.Done():
		return false
	default:
	}

	select {
	case <-ctx.Done():
		return false
	case q.ch <- cmd:
		return true
	}
}

type failingQueue struct{}

func (f *failingQueue) Capacity() int                               { return 1 }
func (f *failingQueue) Length() int                                 { return 0 }
func (f *failingQueue) Enqueue(context.Context, PolledCommand) bool { return false }

func assertCounterValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	got, ok := findCounterValue(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	if got != want {
		t.Fatalf("metric %q = %d, want %d", name, got, want)
	}
}

func findCounterValue(rm metricdata.ResourceMetrics, name string) (int64, bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return 0, false
			}
			var total int64
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
			return total, true
		}
	}
	return 0, false
}

func assertGaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	got, ok := findGaugeValue(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	if got != want {
		t.Fatalf("metric %q = %d, want %d", name, got, want)
	}
}

func findGaugeValue(rm metricdata.ResourceMetrics, name string) (int64, bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			g, ok := m.Data.(metricdata.Gauge[int64])
			if !ok {
				return 0, false
			}
			var total int64
			for _, dp := range g.DataPoints {
				total += dp.Value
			}
			return total, true
		}
	}
	return 0, false
}

func assertCounterValueWithAttributes(t *testing.T, rm metricdata.ResourceMetrics, name string, attr attribute.KeyValue, want int64) {
	t.Helper()
	got, ok := findCounterValueWithAttributes(rm, name, attr)
	if !ok {
		t.Fatalf("metric %q with attribute %q=%q not found", name, attr.Key, attr.Value.AsString())
	}
	if got != want {
		t.Fatalf("metric %q (%q=%q) = %d, want %d", name, attr.Key, attr.Value.AsString(), got, want)
	}
}

func findCounterValueWithAttributes(rm metricdata.ResourceMetrics, name string, attr attribute.KeyValue) (int64, bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				return 0, false
			}
			for _, dp := range sum.DataPoints {
				if attributesContain(dp.Attributes, attr) {
					return dp.Value, true
				}
			}
		}
	}
	return 0, false
}

func attributesContain(set attribute.Set, attr attribute.KeyValue) bool {
	iter := set.Iter()
	for iter.Next() {
		kv := iter.Attribute()
		if kv.Key == attr.Key && kv.Value == attr.Value {
			return true
		}
	}
	return false
}

func assertHistogramCount(t *testing.T, rm metricdata.ResourceMetrics, name string, want int64) {
	t.Helper()
	got, ok := findHistogramCount(rm, name)
	if !ok {
		t.Fatalf("metric %q not found", name)
	}
	if got != want {
		t.Fatalf("metric %q count = %d, want %d", name, got, want)
	}
}

func findHistogramCount(rm metricdata.ResourceMetrics, name string) (int64, bool) {
	for _, scope := range rm.ScopeMetrics {
		for _, m := range scope.Metrics {
			if m.Name != name {
				continue
			}
			h, ok := m.Data.(metricdata.Histogram[float64])
			if !ok {
				return 0, false
			}
			var count int64
			for _, dp := range h.DataPoints {
				count += int64(dp.Count)
			}
			return count, true
		}
	}
	return 0, false
}

type timeoutRecordingFetcher struct {
	mu        sync.Mutex
	callCount int
	durations []time.Duration
}

func (f *timeoutRecordingFetcher) Poll(ctx context.Context, limit int) ([]PolledCommand, types.TunnelServiceRequestID, error) {
	start := time.Now()
	<-ctx.Done()

	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	f.durations = append(f.durations, time.Since(start))
	return nil, "", ctx.Err()
}

func TestPollerPollsWithTimeoutAndRetries(t *testing.T) {
	queue := &chanQueue{ch: make(chan PolledCommand, 1)}
	fetcher := &timeoutRecordingFetcher{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	reader := sdkmetric.NewManualReader()
	meterProvider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() {
		_ = meterProvider.Shutdown(context.Background())
	}()

	pollTimeout := 50 * time.Millisecond
	poller, err := NewPoller(queue, fetcher, logger, meterProvider.Meter("test"), pollTimeout)
	if err != nil {
		t.Fatalf("new poller: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 350*time.Millisecond)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		poller.Run(ctx)
	}()

	wg.Wait()

	fetcher.mu.Lock()
	defer fetcher.mu.Unlock()

	if fetcher.callCount < 2 {
		t.Fatalf("expected poller to retry after timeout, got %d calls", fetcher.callCount)
	}

	for i, duration := range fetcher.durations {
		if duration < pollTimeout/2 {
			t.Fatalf("call %d returned too quickly: %v", i, duration)
		}
		if duration > pollTimeout*2 {
			t.Fatalf("call %d exceeded expected timeout, got %v", i, duration)
		}
	}
}
