package internal

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestForwardingRoundTripperInjectsAndCapturesHeaders(t *testing.T) {
	t.Helper()

	wantRequest := http.Header{"X-Test": {"forward-me"}}
	wantResponse := http.Header{"X-Resp": {"ok"}, "Another": {"value"}}

	rt := NewForwardingRoundTripper(
		roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			got := req.Header.Values("X-Test")
			if diff := cmp.Diff(wantRequest["X-Test"], got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
				t.Fatalf("request headers mismatch (-want +got):\n%s", diff)
			}
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     wantResponse.Clone(),
				Body:       io.NopCloser(strings.NewReader("")),
			}, nil
		}),
	)

	ctx, carrier, err := ContextWithHeaders(context.Background(), wantRequest)
	if err != nil {
		t.Fatalf("ContextWithHeaders: %v", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.com", nil)
	if err != nil {
		t.Fatalf("NewRequestWithContext: %v", err)
	}

	req.Header.Set("X-Test", "should-be-overwritten")

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	_ = resp.Body.Close()

	status, got := carrier.ResponseStatusAndHeaders()
	if diff := cmp.Diff(wantResponse, got, cmpopts.SortSlices(func(a, b string) bool { return a < b })); diff != "" {
		t.Fatalf("response headers mismatch (-want +got):\n%s", diff)
	}

	if status != http.StatusOK {
		t.Fatalf("response status mismatch: got %d, want %d", status, http.StatusOK)
	}
}
