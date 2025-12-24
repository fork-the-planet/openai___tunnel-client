package dispatcherinternal

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.openai.org/api/tunnel-client/pkg/config"
)

func TestFetchOAuthMetadataFallsBackToRoot(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/oauth-protected-resource/base":
			http.NotFound(w, r)
		case "/.well-known/oauth-protected-resource":
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"resource":"%s"}`, r.Host)
		default:
			http.Error(w, "unexpected path", http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)

	baseURL, err := url.Parse(server.URL + "/base")
	require.NoError(t, err)

	cfg := &config.MCPConfig{ServerURL: baseURL}
	require.NoError(t, cfg.BootstrapOAuthResourceMetadataURLs())
	urls := cfg.OAuthResourceMetadataURLs

	client := server.Client()
	client.Timeout = 2 * time.Second

	resp, fetchErr := fetchOAuthMetadata(
		context.Background(),
		client,
		urls,
		slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError})),
	)
	require.NoError(t, fetchErr)
	require.Equal(t, http.StatusOK, resp.ResponseCode())
	require.Equal(t, "application/json", resp.Headers().Get("Content-Type"))
	require.JSONEq(t, fmt.Sprintf(`{"resource":"%s"}`, baseURL.Host), string(resp.Payload()))
}

func TestFetchOAuthMetadataNoURLs(t *testing.T) {
	t.Parallel()

	_, err := fetchOAuthMetadata(context.Background(), &http.Client{Timeout: time.Second}, nil, nil)
	require.Error(t, err)
}
