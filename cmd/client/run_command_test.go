package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandIncludesRun(t *testing.T) {
	t.Parallel()

	root := newRootCommand(func(string) (string, bool) { return "", false }, io.Discard, io.Discard)

	run, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run", run.Name())
	// Flags are registered on the run command itself, not the root command.
	require.NotNil(t, run.PersistentFlags().Lookup("control-plane.base-url"))
}

func TestRunHelpIsScoped(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	root := newRootCommand(func(string) (string, bool) { return "", false }, &stdout, io.Discard)

	root.SetArgs([]string{"run", "--help"})

	require.NoError(t, root.Execute())
	output := stdout.String()
	require.Contains(t, output, "control-plane.base-url")
	require.NotContains(t, output, "Commands:")
}

func TestRunReportsTunnelIDBeforeMissingMCPBinding(t *testing.T) {
	t.Parallel()

	root := newRootCommand(func(key string) (string, bool) {
		switch key {
		case "LOG_FORMAT":
			return "struct-text", true
		case "OPENAI_API_KEY":
			return "dummy-key", true
		default:
			return "", false
		}
	}, io.Discard, io.Discard)

	root.SetArgs([]string{"run"})

	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "tunnel ID is required")
}

func TestRunReportsHowToConfigureMainMCPChannel(t *testing.T) {
	t.Parallel()

	root := newRootCommand(func(key string) (string, bool) {
		switch key {
		case "LOG_FORMAT":
			return "struct-text", true
		case "OPENAI_API_KEY":
			return "dummy-key", true
		case "CONTROL_PLANE_TUNNEL_ID":
			return "tunnel_0123456789abcdef0123456789abcdef", true
		default:
			return "", false
		}
	}, io.Discard, io.Discard)

	root.SetArgs([]string{"run"})

	err := root.Execute()
	require.Error(t, err)
	require.Contains(t, err.Error(), "set --mcp.server-url or --mcp.command")
}
