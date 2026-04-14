package adminui

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultLogExportWindow = 30 * time.Minute
	maxLogExportWindow     = 24 * time.Hour
	metricsSnapshotFile    = "tunnel-client.metrics.prom"
)

type logExportManifest struct {
	GeneratedAt       time.Time        `json:"generated_at"`
	WindowStart       time.Time        `json:"window_start"`
	WindowEnd         time.Time        `json:"window_end"`
	Window            string           `json:"window"`
	EventCount        int              `json:"event_count"`
	LogBufferCapacity int              `json:"log_buffer_capacity"`
	MetricsBytes      int              `json:"metrics_bytes"`
	Redacted          bool             `json:"redacted"`
	Files             []string         `json:"files"`
	Runtime           logExportRuntime `json:"runtime"`
}

type logExportRuntime struct {
	Argv        []string          `json:"argv"`
	Environment map[string]string `json:"environment"`
}

type metricsSnapshot struct {
	Filename string
	Body     []byte
}

// RuntimeSnapshotProvider returns redacted runtime metadata for support log exports.
type RuntimeSnapshotProvider func() logExportRuntime

// MetricsSnapshotProvider returns a point-in-time Prometheus text snapshot for support log exports.
type MetricsSnapshotProvider func() (metricsSnapshot, error)

func NewRuntimeSnapshotProvider() RuntimeSnapshotProvider {
	return func() logExportRuntime {
		return collectLogExportRuntime(os.Args, os.Environ())
	}
}

func NewMetricsSnapshotProvider(exporter http.Handler) MetricsSnapshotProvider {
	return func() (metricsSnapshot, error) {
		if exporter == nil {
			return metricsSnapshot{}, nil
		}

		req, err := http.NewRequest(http.MethodGet, "/metrics", nil)
		if err != nil {
			return metricsSnapshot{}, fmt.Errorf("create metrics snapshot request: %w", err)
		}

		rec := &snapshotResponseWriter{header: make(http.Header)}
		exporter.ServeHTTP(rec, req)
		if rec.statusCode == 0 {
			rec.statusCode = http.StatusOK
		}
		if rec.statusCode != http.StatusOK {
			return metricsSnapshot{}, fmt.Errorf("capture metrics snapshot: unexpected status %d", rec.statusCode)
		}

		return metricsSnapshot{
			Filename: metricsSnapshotFile,
			Body:     bytes.Clone(rec.body.Bytes()),
		}, nil
	}
}

func handleLogsExport(buf *LogBuffer, runtime RuntimeSnapshotProvider, metrics MetricsSnapshotProvider) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		window := parseExportWindow(r)
		now := time.Now().UTC()
		events := buf.Since(now.Add(-window), buf.Capacity())
		snapshot, err := callMetricsSnapshot(metrics)
		if err != nil {
			http.Error(w, "capture metrics snapshot", http.StatusInternalServerError)
			return
		}

		archive, err := buildLogsArchive(events, now, window, buf.Capacity(), callRuntimeSnapshot(runtime), snapshot)
		if err != nil {
			http.Error(w, "build logs archive", http.StatusInternalServerError)
			return
		}

		filename := "tunnel-client-logs-" + now.Format("20060102T150405Z") + ".tar.gz"
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename=%q`, filename))
		w.Header().Set("Content-Length", strconv.Itoa(len(archive)))
		w.Header().Set("Cache-Control", "no-store, max-age=0")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(archive)
	}
}

func callRuntimeSnapshot(runtime RuntimeSnapshotProvider) logExportRuntime {
	if runtime == nil {
		return logExportRuntime{}
	}
	return runtime()
}

func callMetricsSnapshot(metrics MetricsSnapshotProvider) (metricsSnapshot, error) {
	if metrics == nil {
		return metricsSnapshot{}, nil
	}
	return metrics()
}

func buildLogsArchive(events []LogEvent, now time.Time, window time.Duration, logBufferCapacity int, runtime logExportRuntime, snapshot metricsSnapshot) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	files := []string{
		"manifest.json",
		"README.txt",
		"tunnel-client.logs.ndjson",
	}
	if snapshot.Filename != "" {
		files = append(files, snapshot.Filename)
	}

	manifest := logExportManifest{
		GeneratedAt:       now,
		WindowStart:       now.Add(-window),
		WindowEnd:         now,
		Window:            window.String(),
		EventCount:        len(events),
		LogBufferCapacity: logBufferCapacity,
		MetricsBytes:      len(snapshot.Body),
		Redacted:          true,
		Files:             files,
		Runtime:           runtime,
	}

	if err := writeTarJSON(tw, "manifest.json", manifest); err != nil {
		return nil, err
	}
	if err := writeTarFile(tw, "README.txt", []byte("Tunnel-client log export.\n\nLogs are captured from the admin UI in-memory buffer and redacted before export.\nThe NDJSON file contains one redacted JSON log event per line.\nmanifest.json includes redacted argv plus tunnel-client-related environment variables and env: references discovered in argv.\nThe Prometheus snapshot is captured at export time from /metrics.\n")); err != nil {
		return nil, err
	}

	var logs bytes.Buffer
	enc := json.NewEncoder(&logs)
	for _, ev := range events {
		if err := enc.Encode(ev); err != nil {
			return nil, fmt.Errorf("encode log event: %w", err)
		}
	}
	if err := writeTarFile(tw, "tunnel-client.logs.ndjson", logs.Bytes()); err != nil {
		return nil, err
	}
	if snapshot.Filename != "" {
		if err := writeTarFile(tw, snapshot.Filename, snapshot.Body); err != nil {
			return nil, err
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}

func collectLogExportRuntime(argv []string, environ []string) logExportRuntime {
	env := splitEnvironment(environ)
	envKeys := relevantRuntimeEnvKeys(env)
	for key := range envReferencesFromArgs(argv) {
		envKeys[key] = struct{}{}
	}

	outEnv := make(map[string]string, len(envKeys))
	for key := range envKeys {
		val, ok := env[key]
		if !ok {
			continue
		}
		outEnv[key] = redactRuntimeEnv(key, val)
	}

	return logExportRuntime{
		Argv:        redactArgv(argv),
		Environment: outEnv,
	}
}

func splitEnvironment(environ []string) map[string]string {
	out := make(map[string]string, len(environ))
	for _, entry := range environ {
		key, val, ok := strings.Cut(entry, "=")
		if !ok || key == "" {
			continue
		}
		out[key] = val
	}
	return out
}

func relevantRuntimeEnvKeys(env map[string]string) map[string]struct{} {
	out := make(map[string]struct{})
	for key := range env {
		if isRelevantRuntimeEnvKey(key) {
			out[key] = struct{}{}
		}
	}
	return out
}

func isRelevantRuntimeEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	switch upper {
	case "ALLOW_REMOTE_UI",
		"CA_BUNDLE",
		"HEALTH_LISTEN_ADDR",
		"HEALTH_URL_FILE",
		"HTTP_PROXY",
		"HTTPS_PROXY",
		"NO_PROXY",
		"OPENAI_API_KEY",
		"OPEN_WEB_UI",
		"PID_FILE",
		"PROXY_CHECK_INTERVAL":
		return true
	}
	for _, prefix := range []string{
		"ADMIN_UI_",
		"CONTROL_PLANE_",
		"HARPOON_",
		"LOG_",
		"MCP_",
		"OPENAI_TUNNEL_",
		"TUNNEL_",
	} {
		if strings.HasPrefix(upper, prefix) {
			return true
		}
	}
	return false
}

func envReferencesFromArgs(argv []string) map[string]struct{} {
	out := make(map[string]struct{})
	for _, arg := range argv {
		for _, key := range envReferencesFromString(arg) {
			out[key] = struct{}{}
		}
	}
	return out
}

func envReferencesFromString(s string) []string {
	var keys []string
	remaining := s
	for {
		idx := strings.Index(strings.ToLower(remaining), "env:")
		if idx < 0 {
			return keys
		}
		start := idx + len("env:")
		tail := remaining[start:]
		end := 0
		for end < len(tail) && isEnvNameByte(tail[end]) {
			end++
		}
		if end > 0 {
			keys = append(keys, tail[:end])
		}
		remaining = tail[end:]
	}
}

func isEnvNameByte(ch byte) bool {
	return (ch >= 'A' && ch <= 'Z') ||
		(ch >= 'a' && ch <= 'z') ||
		(ch >= '0' && ch <= '9') ||
		ch == '_'
}

func redactArgv(argv []string) []string {
	if len(argv) == 0 {
		return nil
	}
	out := make([]string, 0, len(argv))
	redactNextForKey := ""
	for _, arg := range argv {
		if redactNextForKey != "" {
			out = append(out, redactRuntimeArgValue(redactNextForKey, arg))
			redactNextForKey = ""
			continue
		}

		name, value, hasValue := splitLongFlag(arg)
		if name == "" {
			out = append(out, redactString(arg))
			continue
		}
		if hasValue {
			out = append(out, name+"="+redactRuntimeArgValue(name, value))
			continue
		}
		out = append(out, arg)
		if isSensitiveRuntimeKey(name) {
			redactNextForKey = name
		}
	}
	return out
}

func splitLongFlag(arg string) (name string, value string, hasValue bool) {
	if !strings.HasPrefix(arg, "--") {
		return "", "", false
	}
	if name, value, ok := strings.Cut(arg, "="); ok {
		return name, value, true
	}
	return arg, "", false
}

func redactRuntimeArgValue(key string, value string) string {
	if isSensitiveRuntimeKey(key) && !isReferenceValue(value) {
		return "[REDACTED]"
	}
	return redactString(value)
}

func redactRuntimeEnv(key string, value string) string {
	if isSensitiveRuntimeKey(key) {
		return "[REDACTED]"
	}
	return redactString(value)
}

func isReferenceValue(value string) bool {
	lower := strings.ToLower(strings.TrimSpace(value))
	return strings.HasPrefix(lower, "env:") || strings.HasPrefix(lower, "file:")
}

func isSensitiveRuntimeKey(key string) bool {
	normalized := strings.ToLower(strings.NewReplacer("-", "_", ".", "_").Replace(strings.TrimLeft(key, "-")))
	if isSensitiveAttrKey(normalized) {
		return true
	}
	for _, token := range strings.Split(normalized, "_") {
		switch token {
		case "key", "token", "secret", "password", "cookie", "authorization":
			return true
		}
	}
	return strings.HasSuffix(normalized, "_api_key") || strings.HasSuffix(normalized, "_private_key")
}

type snapshotResponseWriter struct {
	header     http.Header
	body       bytes.Buffer
	statusCode int
}

func (w *snapshotResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *snapshotResponseWriter) Write(p []byte) (int, error) {
	if w.statusCode == 0 {
		w.statusCode = http.StatusOK
	}
	return w.body.Write(p)
}

func (w *snapshotResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func writeTarJSON(tw *tar.Writer, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", name, err)
	}
	data = append(data, '\n')
	return writeTarFile(tw, name, data)
}

func writeTarFile(tw *tar.Writer, name string, data []byte) error {
	hdr := &tar.Header{
		Name:    name,
		Mode:    0o600,
		Size:    int64(len(data)),
		ModTime: time.Now(),
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return fmt.Errorf("write tar header %s: %w", name, err)
	}
	if _, err := tw.Write(data); err != nil {
		return fmt.Errorf("write tar file %s: %w", name, err)
	}
	return nil
}

func parseExportWindow(r *http.Request) time.Duration {
	if r == nil || r.URL == nil {
		return defaultLogExportWindow
	}
	raw := r.URL.Query().Get("minutes")
	if raw == "" {
		return defaultLogExportWindow
	}
	minutes, err := strconv.Atoi(raw)
	if err != nil || minutes <= 0 {
		return defaultLogExportWindow
	}
	window := time.Duration(minutes) * time.Minute
	if window > maxLogExportWindow {
		return maxLogExportWindow
	}
	return window
}
