package mockmcpserver

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/invopop/jsonschema"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type headerContextKey struct{}

// Call defines a scripted tool invocation.
type Call struct {
	Tool            string
	Result          json.RawMessage
	DynamicResult   func(json.RawMessage) (json.RawMessage, error)
	Error           *CallError
	Progress        []ProgressUpdate
	ResponseHeaders http.Header
}

// CallError describes the error payload returned by the tool.
type CallError struct {
	Message string
}

// ProgressUpdate emits a notifications/progress event during a streaming response.
type ProgressUpdate struct {
	Percentage float64
	Message    string
}

// IncomingRequest captures the tool arguments and headers that reached the mock.
type IncomingRequest struct {
	Tool      string
	Arguments json.RawMessage
	Headers   http.Header
}

// MockMCPServer hosts a Streamable HTTP MCP server backed by scripted tool handlers.
type Option func(*MockMCPServer)

// WithCalls seeds the mock with scripted tool invocations.
func WithCalls(calls ...Call) Option {
	return func(m *MockMCPServer) {
		m.mu.Lock()
		defer m.mu.Unlock()
		for i := range calls {
			m.calls = append(m.calls, cloneCall(calls[i]))
		}
	}
}

// WithKeepalivePings injects SSE keepalive ping events ahead of streamable responses.
func WithKeepalivePings() Option {
	return func(m *MockMCPServer) {
		m.injectKeepalivePings = true
	}
}

type MockMCPServer struct {
	mu       sync.Mutex
	calls    []*Call
	received []IncomingRequest

	server     *mcp.Server
	httpServer *httptest.Server
	baseURL    *url.URL
	closeOnce  sync.Once

	injectKeepalivePings bool

	tb atomic.Value // testing.TB
}

// NewMockMCPServer constructs an empty mock server configured by optional options.
func NewMockMCPServer(opts ...Option) *MockMCPServer {
	mock := &MockMCPServer{}
	for _, opt := range opts {
		opt(mock)
	}
	return mock
}

// Start launches the mock MCP server and registers cleanup with t.
func (m *MockMCPServer) Start(t testing.TB) {
	t.Helper()
	m.mu.Lock()
	if m.httpServer != nil {
		m.mu.Unlock()
		t.Fatalf("mock MCP server already started")
		return
	}

	server := mcp.NewServer(&mcp.Implementation{Name: "mock-mcp-server", Version: "1.0.0"}, nil)
	tools := m.uniqueToolsLocked()
	for _, toolName := range tools {
		tool := &mcp.Tool{
			Name:        toolName,
			Description: "mock tool",
			InputSchema: &jsonschema.Schema{Type: "object"},
		}
		mcp.AddTool(server, tool, m.toolHandler(toolName))
	}

	handler := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server { return server }, nil)
	httpHandler := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method == http.MethodPost {
			body, err := io.ReadAll(req.Body)
			if err != nil {
				m.failf("mock MCP server read body: %v", err)
			}
			_ = req.Body.Close()
			var methodProbe struct {
				Method string `json:"method"`
			}
			if err := json.Unmarshal(body, &methodProbe); err == nil && methodProbe.Method == "notifications/initialized" {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			req.Body = io.NopCloser(bytes.NewReader(body))
			hw := &headerWriter{ResponseWriter: w}
			w = hw
			req = req.WithContext(context.WithValue(req.Context(), headerContextKey{}, hw))
		}
		if m.injectKeepalivePings && req.Method == http.MethodGet && acceptsEventStream(req) {
			w = &keepalivePingWriter{ResponseWriter: w}
		}
		handler.ServeHTTP(w, req)
	})

	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		m.mu.Unlock()
		t.Skipf("mock MCP server listener unavailable: %v", err)
		return
	}
	httpServer := &httptest.Server{
		Listener: listener,
		Config:   &http.Server{Handler: httpHandler},
	}
	httpServer.Start()

	parsed, err := url.Parse(httpServer.URL)
	if err != nil {
		m.mu.Unlock()
		httpServer.Close()
		t.Fatalf("mock MCP server parse URL: %v", err)
		return
	}
	m.server = server
	m.httpServer = httpServer
	m.baseURL = parsed
	m.mu.Unlock()

	m.tb.Store(t)
	t.Cleanup(m.Close)
}

// Close shuts down the server and asserts all scripted calls were consumed.
func (m *MockMCPServer) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		server := m.httpServer
		m.httpServer = nil
		m.baseURL = nil
		remaining := len(m.calls)
		m.calls = nil
		m.mu.Unlock()
		if server != nil {
			server.Close()
		}
		if remaining != 0 {
			m.failf("mock MCP server stopped with %d pending call(s)", remaining)
		}
	})
}

// BaseURL returns the HTTP endpoint the MCP client should connect to.
func (m *MockMCPServer) BaseURL() *url.URL {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.baseURL == nil {
		return nil
	}
	copyURL := *m.baseURL
	return &copyURL
}

// WaitForRequests blocks until at least n tool calls have completed or ctx expires.
func (m *MockMCPServer) WaitForRequests(ctx context.Context, n int) error {
	if n <= 0 {
		return nil
	}
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		m.mu.Lock()
		count := len(m.received)
		m.mu.Unlock()
		if count >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// ReceivedRequests returns the recorded tool requests in order.
func (m *MockMCPServer) ReceivedRequests() []IncomingRequest {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]IncomingRequest, len(m.received))
	for i, req := range m.received {
		out[i] = IncomingRequest{
			Tool:      req.Tool,
			Arguments: cloneJSON(req.Arguments),
			Headers:   cloneHeader(req.Headers),
		}
	}
	return out
}

func (m *MockMCPServer) uniqueToolsLocked() []string {
	seen := make(map[string]struct{})
	for _, call := range m.calls {
		if call.Tool != "" {
			seen[call.Tool] = struct{}{}
		}
	}
	tools := make([]string, 0, len(seen))
	for name := range seen {
		tools = append(tools, name)
	}
	return tools
}

func (m *MockMCPServer) toolHandler(name string) mcp.ToolHandlerFor[map[string]any, map[string]any] {
	return func(ctx context.Context, req *mcp.CallToolRequest, _ map[string]any) (*mcp.CallToolResult, map[string]any, error) {
		call := m.popCall(name)
		if call == nil {
			return nil, nil, fmt.Errorf("no scripted response for tool %q", name)
		}
		if hw, ok := ctx.Value(headerContextKey{}).(*headerWriter); ok {
			hw.addHeaders(call.ResponseHeaders)
		}
		m.recordRequest(req, call)
		if call.DynamicResult != nil {
			payload, err := call.DynamicResult(req.Params.Arguments)
			if err != nil {
				return nil, nil, err
			}
			call.Result = cloneJSON(payload)
		}
		for _, update := range call.Progress {
			params := &mcp.ProgressNotificationParams{
				Progress: update.Percentage,
				Message:  update.Message,
			}
			if err := req.Session.NotifyProgress(ctx, params); err != nil {
				return nil, nil, err
			}
		}
		result, structured, err := buildResult(call)
		if err != nil {
			return nil, nil, err
		}
		if call.Error != nil {
			result.IsError = true
			if len(result.Content) == 0 {
				result.Content = []mcp.Content{&mcp.TextContent{Text: call.Error.Message}}
			}
		}
		return result, structured, nil
	}
}

func buildResult(call *Call) (*mcp.CallToolResult, map[string]any, error) {
	if call.Error != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: call.Error.Message}},
			IsError: true,
		}, nil, nil
	}
	if len(call.Result) == 0 {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: "ok"}},
		}, map[string]any{}, nil
	}
	var structured map[string]any
	if err := json.Unmarshal(call.Result, &structured); err != nil {
		return nil, nil, fmt.Errorf("decode call result: %w", err)
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: string(call.Result)}},
	}, structured, nil
}

func (m *MockMCPServer) recordRequest(req *mcp.CallToolRequest, call *Call) {
	m.mu.Lock()
	defer m.mu.Unlock()
	args := cloneJSON(req.Params.Arguments)
	headers := cloneHeader(req.Extra.Header)
	m.received = append(m.received, IncomingRequest{
		Tool:      call.Tool,
		Arguments: args,
		Headers:   headers,
	})
}

func (m *MockMCPServer) popCall(tool string) *Call {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, call := range m.calls {
		if call.Tool == tool {
			m.calls = append(m.calls[:i], m.calls[i+1:]...)
			return call
		}
	}
	return nil
}

func cloneCall(call Call) *Call {
	if call.ResponseHeaders != nil {
		call.ResponseHeaders = cloneHeader(call.ResponseHeaders)
	}
	if len(call.Result) > 0 {
		call.Result = cloneJSON(call.Result)
	}
	if len(call.Progress) > 0 {
		prog := make([]ProgressUpdate, len(call.Progress))
		copy(prog, call.Progress)
		call.Progress = prog
	}
	c := call
	return &c
}

func cloneJSON(raw json.RawMessage) json.RawMessage {
	if raw == nil {
		return nil
	}
	out := make([]byte, len(raw))
	copy(out, raw)
	return json.RawMessage(out)
}

func cloneHeader(h http.Header) http.Header {
	if h == nil {
		return nil
	}
	out := make(http.Header, len(h))
	for k, values := range h {
		copyVals := make([]string, len(values))
		copy(copyVals, values)
		out[k] = copyVals
	}
	return out
}

type headerWriter struct {
	http.ResponseWriter
	headers http.Header
	wrote   bool
}

func (h *headerWriter) WriteHeader(status int) {
	if !h.wrote {
		for key, values := range h.headers {
			for _, value := range values {
				h.ResponseWriter.Header().Add(key, value)
			}
		}
		h.wrote = true
	}
	h.ResponseWriter.WriteHeader(status)
}

func (h *headerWriter) Write(b []byte) (int, error) {
	if !h.wrote {
		h.WriteHeader(http.StatusOK)
	}
	return h.ResponseWriter.Write(b)
}

func (h *headerWriter) addHeaders(src http.Header) {
	if src == nil {
		return
	}
	if h.headers == nil {
		h.headers = make(http.Header)
	}
	for key, values := range src {
		h.headers[key] = append(h.headers[key], values...)
	}
}

func acceptsEventStream(req *http.Request) bool {
	accept := strings.Split(strings.Join(req.Header.Values("Accept"), ","), ",")
	for _, candidate := range accept {
		switch strings.TrimSpace(candidate) {
		case "text/event-stream", "text/*", "*/*":
			return true
		}
	}
	return false
}

type keepalivePingWriter struct {
	http.ResponseWriter
	injected bool
}

func (w *keepalivePingWriter) WriteHeader(status int) {
	w.ResponseWriter.WriteHeader(status)
	w.injectPing()
}

func (w *keepalivePingWriter) Write(p []byte) (int, error) {
	w.injectPing()
	return w.ResponseWriter.Write(p)
}

func (w *keepalivePingWriter) Flush() {
	w.injectPing()
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *keepalivePingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if hj, ok := w.ResponseWriter.(http.Hijacker); ok {
		return hj.Hijack()
	}
	return nil, nil, fmt.Errorf("response writer does not support hijacking")
}

func (w *keepalivePingWriter) injectPing() {
	if w.injected {
		return
	}
	w.injected = true
	_, _ = w.ResponseWriter.Write([]byte("event: ping\n"))
	_, _ = w.ResponseWriter.Write([]byte("data: ping\n\n"))
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (m *MockMCPServer) failf(format string, args ...any) {
	if tb, ok := m.tb.Load().(testing.TB); ok && tb != nil {
		tb.Helper()
		tb.Fatalf(format, args...)
		return
	}
	panic(fmt.Sprintf(format, args...))
}
