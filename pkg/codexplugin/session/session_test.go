package session

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"go.openai.org/api/tunnel-client/pkg/codexplugin/state"
)

type fakeProcess struct {
	pid      int
	exitCode *int
}

func (p *fakeProcess) PID() int   { return p.pid }
func (p *fakeProcess) Poll() *int { return p.exitCode }

func TestWriteRuntimeProfileUsesExistingJSONCompatibleShape(t *testing.T) {
	t.Parallel()

	root := state.Root{Path: t.TempDir()}
	path, err := WriteRuntimeProfile(
		"docs-mcp",
		"",
		"tunnel_123",
		"https://api.openai.com",
		"env:CONTROL_PLANE_API_KEY",
		Target{Kind: "server_url", Value: "http://127.0.0.1:3001/mcp"},
		filepath.Join(t.TempDir(), "profiles"),
		root,
		nil,
	)
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Contains(t, string(data), `"config_version": 1`)
	require.Contains(t, string(data), `"server_urls": [`)
}

func TestTmuxSessionNameIsScopedByStateRoot(t *testing.T) {
	t.Parallel()

	first := TmuxSessionName("docs-mcp", state.Root{Path: "/tmp/one"})
	second := TmuxSessionName("docs-mcp", state.Root{Path: "/tmp/two"})
	require.NotEqual(t, first, second)
	require.Contains(t, first, "tunnel-mcp__docs-mcp__")
}

func TestProbeHealthEndpoints(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("live"))
		case "/readyz":
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte("pending"))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	probe := ProbeHealthEndpoints(server.URL + "/healthz")
	require.True(t, probe.Healthz.OK)
	require.False(t, probe.Readyz.OK)
	require.Equal(t, http.StatusServiceUnavailable, probe.Readyz.Status)
}

func TestStartOrReuseFallsBackToProcessMode(t *testing.T) {
	t.Parallel()

	root := state.Root{Path: t.TempDir()}
	require.NoError(t, state.EnsureDirs(root))

	healthURL := "http://127.0.0.1:43199/healthz"
	require.NoError(t, os.WriteFile(ProfileHealthURLFile("docs-mcp", root), []byte(healthURL), 0o600))
	rt := Runtime{
		Run: func(args []string, env map[string]string) (CompletedProcess, error) {
			if len(args) >= 2 && args[0] == "tmux" && args[1] == "-V" {
				return CompletedProcess{}, os.ErrNotExist
			}
			return CompletedProcess{}, nil
		},
		Start: func(args []string, env map[string]string, logPath string) (Process, error) {
			return &fakeProcess{pid: os.Getpid()}, nil
		},
	}

	result, err := StartOrReuse(rt, "docs-mcp", "docs-mcp", t.TempDir(), "/bin/tunnel-client", root, nil, 0, false)
	require.NoError(t, err)
	require.Equal(t, "process", result.Mode)
	require.True(t, result.Launched)
	require.Equal(t, os.Getpid(), result.PID)
}
