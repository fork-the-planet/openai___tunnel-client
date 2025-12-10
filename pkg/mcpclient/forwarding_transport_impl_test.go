package mcpclient

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/modelcontextprotocol/go-sdk/jsonrpc"

	"go.openai.org/api/tunnel-client/pkg/mcpclient/internal"
)

func TestForwardingConnectionPropagatesHeaders(t *testing.T) {
	respHeaders := http.Header{"X-Response": {"ok"}, "Another": {"value"}}
	const wantStatus = http.StatusAccepted
	sortStrings := cmpopts.SortSlices(func(a, b string) bool { return a < b })

	callID := mustMakeID(t, "call-1")

	fake := &fakeConnection{
		writeFunc: func(ctx context.Context, msg jsonrpc.Message) error {
			carrier := internal.CarrierFromContext(ctx)
			if carrier == nil {
				t.Fatalf("carrier missing in context")
			}
			carrier.StoreResponse(wantStatus, respHeaders)
			return nil
		},
		readFunc: func(ctx context.Context) (jsonrpc.Message, error) {
			return &jsonrpc.Response{
				ID: callID,
			}, nil
		},
	}

	conn := &forwardingConnection{
		base: fake,
	}

	req := &jsonrpc.Request{
		ID:     callID,
		Method: "testMethod",
	}

	requestHeaders := http.Header{"X-Forward": {"value"}}

	statusCode, gotWriteHeaders, err := conn.Write(context.Background(), requestHeaders, req)
	if err != nil {
		t.Fatalf("Write returned error: %v", err)
	}
	if statusCode != wantStatus {
		t.Fatalf("unexpected status code: got %d, want %d", statusCode, wantStatus)
	}
	if diff := cmp.Diff(respHeaders, gotWriteHeaders, sortStrings); diff != "" {
		t.Fatalf("write headers mismatch (-want +got):\n%s", diff)
	}

	msg, err := conn.Read(context.Background())
	if err != nil {
		t.Fatalf("Read returned error: %v", err)
	}
	if _, ok := msg.(*jsonrpc.Response); !ok {
		t.Fatalf("expected jsonrpc.Response, got %T", msg)
	}

	if fake.lastForwardedHeader == nil {
		t.Fatalf("request headers were not forwarded to fake connection")
	}
	if diff := cmp.Diff(requestHeaders, fake.lastForwardedHeader, sortStrings); diff != "" {
		t.Fatalf("request headers mismatch (-want +got):\n%s", diff)
	}
}

type fakeConnection struct {
	writeFunc           func(context.Context, jsonrpc.Message) error
	readFunc            func(context.Context) (jsonrpc.Message, error)
	lastForwardedHeader http.Header
}

func (f *fakeConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if f.readFunc != nil {
		return f.readFunc(ctx)
	}
	return nil, nil
}

func (f *fakeConnection) Write(ctx context.Context, msg jsonrpc.Message) error {
	if carrier := internal.CarrierFromContext(ctx); carrier != nil {
		f.lastForwardedHeader = carrier.RequestHeaders()
	}
	if f.writeFunc == nil {
		return nil
	}
	return f.writeFunc(ctx, msg)
}

func (f *fakeConnection) Close() error      { return nil }
func (f *fakeConnection) SessionID() string { return "" }

func mustMakeID(tb testing.TB, v any) jsonrpc.ID {
	tb.Helper()
	id, err := jsonrpc.MakeID(v)
	if err != nil {
		tb.Fatalf("jsonrpc.MakeID(%v): %v", v, err)
	}
	return id
}
