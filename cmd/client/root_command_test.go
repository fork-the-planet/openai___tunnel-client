package main

import (
	"bytes"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRootCommandWithNoArgsPrintsHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	root := newRootCommand(func(string) (string, bool) { return "", false }, &stdout, io.Discard)

	root.SetArgs([]string{})

	require.NoError(t, root.Execute())
	output := stdout.String()
	require.Contains(t, output, "Commands:")
	require.Contains(t, output, "run")
	require.Contains(t, output, "connect a local or private MCP server")
	require.Contains(t, output, "codex")
	require.Contains(t, output, "sessions")
	require.Contains(t, output, "admin-profiles")
}

func TestRootHelpListsSubcommands(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	root := newRootCommand(func(string) (string, bool) { return "", false }, &stdout, io.Discard)

	root.SetArgs([]string{"--help"})

	require.NoError(t, root.Execute())
	output := stdout.String()
	require.Contains(t, output, "Commands:")
	require.Contains(t, output, "run")
	require.Contains(t, output, "dev")
	require.Contains(t, output, "codex")
	require.Contains(t, output, "sessions")
	require.Contains(t, output, "admin-profiles")
	require.NotContains(t, output, "control-plane.base-url")
}
