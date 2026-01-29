package adminui

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/harpoon"
)

func TestBuildHarpoonStatusDisabledWithoutTargets(t *testing.T) {
	registry, err := harpoon.NewRegistry(true, nil)
	require.NoError(t, err)

	out := buildHarpoonStatus(registry, &config.HarpoonConfig{})
	require.False(t, out.Enabled)
	require.Equal(t, "no targets configured", out.Reason)
}

func TestBuildHarpoonTargetsIncludesConfig(t *testing.T) {
	registry, err := harpoon.NewRegistry(true, []harpoon.Target{{
		Label:       "auth",
		Description: "Auth service",
		BaseURL:     mustParseURL(t, "http://example.com/base"),
	}})
	require.NoError(t, err)

	cfg := &config.HarpoonConfig{
		AllowPlaintextHTTP: true,
		MaxResponseBytes:   123,
		MaxRedirects:       4,
	}

	out := buildHarpoonTargets(registry, cfg)
	require.Len(t, out.Targets, 1)
	require.Equal(t, "auth", out.Targets[0].Label)
	require.Equal(t, "http://example.com/base", out.Targets[0].URL)
	require.True(t, out.Targets[0].AllowPlaintextHTTP)
	require.Equal(t, 123, out.Targets[0].MaxResponseBytes)
	require.Equal(t, 4, out.Targets[0].MaxRedirects)
}

func TestBuildHarpoonCallsIncludesPayloadsWhenEnabled(t *testing.T) {
	buffer := harpoon.NewCallBuffer()
	buffer.RecordCall(harpoon.CallEntry{
		Timestamp:    time.Unix(10, 0).UTC(),
		Label:        "auth",
		URL:          "https://example.com/token",
		Method:       "POST",
		Status:       200,
		LatencyMS:    30,
		ReqBytes:     10,
		RespBytes:    20,
		RequestBody:  `{"a":1}`,
		ResponseBody: `{"ok":true}`,
		BodyIsBase64: false,
	})

	out := buildHarpoonCalls(buffer, &config.HarpoonConfig{CapturePayloads: true}, "auth", 10)
	require.Len(t, out.Calls, 1)
	require.NotNil(t, out.Calls[0].RequestBody)
	require.Equal(t, `{"a":1}`, *out.Calls[0].RequestBody)
	require.NotNil(t, out.Calls[0].BodyIsBase64)
	require.False(t, *out.Calls[0].BodyIsBase64)
}

func TestBuildHarpoonCallsOmitsPayloadsWhenDisabled(t *testing.T) {
	buffer := harpoon.NewCallBuffer()
	buffer.RecordCall(harpoon.CallEntry{
		Timestamp: time.Unix(10, 0).UTC(),
		Label:     "auth",
		URL:       "https://example.com/token",
		Method:    "POST",
		Status:    200,
	})

	out := buildHarpoonCalls(buffer, &config.HarpoonConfig{CapturePayloads: false}, "", 10)
	require.Len(t, out.Calls, 1)
	require.Nil(t, out.Calls[0].RequestBody)
	require.Nil(t, out.Calls[0].ResponseBody)
	require.Nil(t, out.Calls[0].BodyIsBase64)
}
