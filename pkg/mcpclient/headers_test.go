package mcpclient

import (
	"net/http"
	"testing"
)

func TestFindHeaderValue(t *testing.T) {

	testCases := []struct {
		name    string
		headers http.Header
		target  string
		want    *string
	}{
		{
			name:    "returns nil when headers map empty",
			headers: http.Header{},
			target:  "X-Test",
			want:    nil,
		},
		{
			name:    "returns nil when header missing",
			headers: http.Header{"Some-Other": {"value"}},
			target:  "X-Test",
			want:    nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := FindHeaderValue(tc.headers, tc.target)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("expected nil, got %q", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("expected %q, got nil", *tc.want)
			case tc.want != nil && got != nil && *tc.want != *got:
				t.Fatalf("expected %q, got %q", *tc.want, *got)
			}
		})
	}
}

func TestSessionIDFromHeaders(t *testing.T) {
	ptr := func(v string) *string { return &v }

	testCases := []struct {
		name    string
		headers http.Header
		want    *string
	}{
		{
			name:    "returns nil when missing",
			headers: http.Header{},
			want:    nil,
		},
		{
			name:    "returns session id when present",
			headers: http.Header{HeaderSessionID: {"session-123"}},
			want:    ptr("session-123"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			got := SessionIDFromHeaders(tc.headers)
			switch {
			case tc.want == nil && got != nil:
				t.Fatalf("expected nil, got %q", *got)
			case tc.want != nil && got == nil:
				t.Fatalf("expected %q, got nil", *tc.want)
			case tc.want != nil && got != nil && *tc.want != *got:
				t.Fatalf("expected %q, got %q", *tc.want, *got)
			}
		})
	}
}
