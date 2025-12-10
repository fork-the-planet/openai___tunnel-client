package mockmcpserver

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestMockMCPServerUsage(t *testing.T) {
	t.Parallel()

	server := NewMockMCPServer(
		WithCalls(
			Call{
				Tool:   "ping",
				Result: json.RawMessage(`{"message":"pong"}`),
				ResponseHeaders: http.Header{
					"X-Mcp-Mode": {"simple"},
				},
			},
			Call{
				Tool: "stream",
				Progress: []ProgressUpdate{
					{Percentage: 50, Message: "halfway"},
				},
				Result: json.RawMessage(`{"complete":true}`),
				ResponseHeaders: http.Header{
					"X-Mcp-Mode": {"stream"},
				},
			},
		),
	)
	server.Start(t)

	baseURL := server.BaseURL()
	if baseURL == nil {
		t.Fatal("mock MCP server did not expose a base URL")
	}

	var (
		progressMu sync.Mutex
		progress   []float64
		progressCh = make(chan struct{}, 1)
	)
	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0.1"}, &mcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, req *mcp.ProgressNotificationClientRequest) {
			progressMu.Lock()
			progress = append(progress, req.Params.Progress)
			if len(progress) == 1 {
				select {
				case progressCh <- struct{}{}:
				default:
				}
			}
			progressMu.Unlock()
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: baseURL.String()}, nil)
	if err != nil {
		t.Fatalf("connect MCP client: %v", err)
	}
	defer func() {
		_ = session.Close()
	}()

	pingRes, err := session.CallTool(ctx, &mcp.CallToolParams{Name: "ping"})
	if err != nil {
		t.Fatalf("call ping: %v", err)
	}
	var pingStructured map[string]any
	rawPing, err := json.Marshal(pingRes.StructuredContent)
	if err == nil {
		_ = json.Unmarshal(rawPing, &pingStructured)
	}
	if pingStructured["message"] != "pong" {
		t.Fatalf("unexpected ping result: %v", pingStructured)
	}

	progressMu.Lock()
	progress = progress[:0]
	progressMu.Unlock()

	streamParams := &mcp.CallToolParams{Name: "stream", Arguments: map[string]any{"topic": "demo"}}
	streamParams.SetProgressToken("stream-progress")
	streamRes, err := session.CallTool(ctx, streamParams)
	if err != nil {
		t.Fatalf("call stream: %v", err)
	}
	var streamStructured map[string]any
	if raw, err := json.Marshal(streamRes.StructuredContent); err == nil {
		_ = json.Unmarshal(raw, &streamStructured)
	}
	if streamStructured["complete"] != true {
		t.Fatalf("unexpected stream result: %v", streamStructured)
	}
	select {
	case <-progressCh:
	case <-time.After(time.Second):
		progressMu.Lock()
		defer progressMu.Unlock()
		t.Fatalf("expected progress notification, got %v", progress)
	}
	progressMu.Lock()
	firstProgress := progress[0]
	progressMu.Unlock()
	if firstProgress != 50 {
		t.Fatalf("unexpected progress updates: %v", progress)
	}

	waitCtx, waitCancel := context.WithTimeout(context.Background(), time.Second)
	defer waitCancel()
	if err := server.WaitForRequests(waitCtx, 2); err != nil {
		t.Fatalf("wait for requests: %v", err)
	}
	reqs := server.ReceivedRequests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 recorded requests, got %d", len(reqs))
	}
	if reqs[0].Tool != "ping" || reqs[1].Tool != "stream" {
		t.Fatalf("unexpected request order: %+v", reqs)
	}
}
