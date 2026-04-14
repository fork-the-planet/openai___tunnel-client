package adminui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestHandleLogsExportReturnsRedactedTarGz(t *testing.T) {
	t.Parallel()

	buf := NewLogBufferWithCapacity(10)
	r := slog.NewRecord(time.Now(), slog.LevelInfo, "request Authorization: Bearer sk-proj-abcdefg1234567890", 0)
	r.AddAttrs(
		slog.String("api_key", "sk-proj-secretvalue123456"),
		slog.String("raw", "standalone sk-proj-standalone123456"),
	)
	buf.Handle(context.Background(), r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/export?minutes=30", nil)
	handleLogsExport(buf, func() logExportRuntime {
		return collectLogExportRuntime(
			[]string{"tunnel-client", "run", "--control-plane.api-key=env:OPENAI_TUNNEL_KEY_PROD"},
			[]string{
				"CONTROL_PLANE_TUNNEL_ID=tunnel_0123456789abcdef0123456789abcdef",
				"OPENAI_TUNNEL_KEY_PROD=sk-proj-runtime-secret123456",
			},
		)
	}, func() (metricsSnapshot, error) {
		return metricsSnapshot{
			Filename: metricsSnapshotFile,
			Body:     []byte("# HELP test_metric test\n# TYPE test_metric counter\ntest_metric 7\n"),
		}, nil
	})(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/gzip", rec.Header().Get("Content-Type"))
	require.Regexp(t, `^attachment; filename="tunnel-client-logs-\d{8}T\d{6}Z\.tar\.gz"$`, rec.Header().Get("Content-Disposition"))
	require.NotEmpty(t, rec.Header().Get("Content-Length"))

	files := readTarGzForTest(t, rec.Body.Bytes())
	require.Contains(t, files, "manifest.json")
	require.Contains(t, files, "README.txt")
	require.Contains(t, files, "tunnel-client.logs.ndjson")
	require.Contains(t, files, metricsSnapshotFile)

	require.Contains(t, files["tunnel-client.logs.ndjson"], "sk-REDACTED")
	require.Contains(t, files["tunnel-client.logs.ndjson"], "Authorization: Bearer [REDACTED]")
	require.Contains(t, files["tunnel-client.logs.ndjson"], `"api_key":"[REDACTED]"`)
	require.NotContains(t, files["tunnel-client.logs.ndjson"], "secretvalue")
	require.Contains(t, files[metricsSnapshotFile], "test_metric 7")

	var manifest logExportManifest
	require.NoError(t, json.Unmarshal([]byte(files["manifest.json"]), &manifest))
	require.True(t, manifest.Redacted)
	require.Equal(t, 1, manifest.EventCount)
	require.Equal(t, 10, manifest.LogBufferCapacity)
	require.Equal(t, len(files[metricsSnapshotFile]), manifest.MetricsBytes)
	require.Contains(t, manifest.Files, metricsSnapshotFile)
	require.Contains(t, manifest.Runtime.Argv, "--control-plane.api-key=env:OPENAI_TUNNEL_KEY_PROD")
	require.Equal(t, "[REDACTED]", manifest.Runtime.Environment["OPENAI_TUNNEL_KEY_PROD"])
}

func TestBuildLogsArchiveFiltersBeforeCallSite(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	archive, err := buildLogsArchive([]LogEvent{
		{Seq: 7, Time: now, Level: "INFO", Message: "hello"},
	}, now, 30*time.Minute, 2000, logExportRuntime{Argv: []string{"tunnel-client", "run"}}, metricsSnapshot{
		Filename: metricsSnapshotFile,
		Body:     []byte("commands_poll_cycles_total 42\n"),
	})
	require.NoError(t, err)

	files := readTarGzForTest(t, archive)
	require.Contains(t, files["tunnel-client.logs.ndjson"], `"seq":7`)
	require.Contains(t, files["tunnel-client.logs.ndjson"], "hello")
	require.Equal(t, "commands_poll_cycles_total 42\n", files[metricsSnapshotFile])
}

func TestBuildLogsArchiveOmitsMetricsFileWhenSnapshotUnavailable(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	archive, err := buildLogsArchive(nil, now, 15*time.Minute, 128, logExportRuntime{}, metricsSnapshot{})
	require.NoError(t, err)

	files := readTarGzForTest(t, archive)
	require.NotContains(t, files, metricsSnapshotFile)

	var manifest logExportManifest
	require.NoError(t, json.Unmarshal([]byte(files["manifest.json"]), &manifest))
	require.Zero(t, manifest.MetricsBytes)
	require.NotContains(t, manifest.Files, metricsSnapshotFile)
}

func TestCollectLogExportRuntimeKeepsReproMetadataAndRedactsSecrets(t *testing.T) {
	t.Parallel()

	got := collectLogExportRuntime(
		[]string{
			"tunnel-client",
			"run",
			"--control-plane.api-key=env:OPENAI_TUNNEL_KEY_PROD",
			"--mcp.server-url",
			"https://example.test/mcp?code=secret-code",
			"--harpoon.target=url=https://target.test?access_token=target-token",
			"--control-plane.extra-headers=X-Tunnel-Shard-Token: shard-secret",
			"--admin-token",
			"literal-admin-token",
			"--unrelated",
			"sk-proj-argv-secret123456",
		},
		[]string{
			"CONTROL_PLANE_TUNNEL_ID=tunnel_0123456789abcdef0123456789abcdef",
			"LOG_LEVEL=debug",
			"MCP_SERVER_URL=https://env.example/mcp",
			"HTTPS_PROXY=http://proxy-user:proxy-pass@proxy.example:8080",
			"OPENAI_TUNNEL_KEY_PROD=sk-proj-env-secret123456",
			"UNRELATED_SECRET=should-not-be-exported-because-not-relevant",
		},
	)

	require.Contains(t, got.Argv, "--control-plane.api-key=env:OPENAI_TUNNEL_KEY_PROD")
	require.Contains(t, got.Argv, "https://example.test/mcp?code=[REDACTED]")
	require.Contains(t, got.Argv, "--harpoon.target=url=https://target.test?access_token=[REDACTED]")
	require.Contains(t, got.Argv, "--control-plane.extra-headers=X-Tunnel-Shard-Token: [REDACTED]")
	require.Contains(t, got.Argv, "[REDACTED]")
	require.Contains(t, got.Argv, "sk-REDACTED")
	require.NotContains(t, got.Argv, "literal-admin-token")
	require.NotContains(t, got.Argv, "sk-proj-argv-secret123456")

	require.Equal(t, "tunnel_0123456789abcdef0123456789abcdef", got.Environment["CONTROL_PLANE_TUNNEL_ID"])
	require.Equal(t, "debug", got.Environment["LOG_LEVEL"])
	require.Equal(t, "https://env.example/mcp", got.Environment["MCP_SERVER_URL"])
	require.Equal(t, "http://[REDACTED]@proxy.example:8080", got.Environment["HTTPS_PROXY"])
	require.Equal(t, "[REDACTED]", got.Environment["OPENAI_TUNNEL_KEY_PROD"])
	require.NotContains(t, got.Environment, "UNRELATED_SECRET")
	require.NotContains(t, got.Environment, "should-not-be-exported-because-not-relevant")
}

func TestHandleLogsExportReturnsInternalServerErrorWhenMetricsSnapshotFails(t *testing.T) {
	t.Parallel()

	buf := NewLogBufferWithCapacity(4)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/logs/export?minutes=30", nil)

	handleLogsExport(buf, nil, func() (metricsSnapshot, error) {
		return metricsSnapshot{}, errors.New("boom")
	})(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "capture metrics snapshot")
}

func TestNewMetricsSnapshotProviderCapturesHandlerOutput(t *testing.T) {
	t.Parallel()

	provider := NewMetricsSnapshotProvider(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("queue_depth 3\n"))
	}))

	got, err := provider()
	require.NoError(t, err)
	require.Equal(t, metricsSnapshotFile, got.Filename)
	require.Equal(t, []byte("queue_depth 3\n"), got.Body)
}

func TestNewMetricsSnapshotProviderRejectsUnexpectedStatus(t *testing.T) {
	t.Parallel()

	provider := NewMetricsSnapshotProvider(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))

	_, err := provider()
	require.EqualError(t, err, "capture metrics snapshot: unexpected status 503")
}

func readTarGzForTest(t *testing.T, data []byte) map[string]string {
	t.Helper()

	gz, err := gzip.NewReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() {
		require.NoError(t, gz.Close())
	}()

	tr := tar.NewReader(gz)
	files := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		body, err := io.ReadAll(tr)
		require.NoError(t, err)
		files[hdr.Name] = string(body)
	}
	return files
}
