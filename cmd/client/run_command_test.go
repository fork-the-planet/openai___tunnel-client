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

	cmd, _, err := root.Find([]string{"run"})
	require.NoError(t, err)
	require.Equal(t, "run", cmd.Name())
	require.NotNil(t, root.PersistentFlags().Lookup("control-plane.base-url"))
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
	require.Contains(t, output, "control-plane.base-url")
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

func TestRootCommandWithNoArgsPrintsHelp(t *testing.T) {
	t.Parallel()

	var stdout bytes.Buffer
	root := newRootCommand(func(string) (string, bool) { return "", false }, &stdout, io.Discard)

	root.SetArgs([]string{})

	require.NoError(t, root.Execute())
	output := stdout.String()
	require.Contains(t, output, "Commands:")
	require.Contains(t, output, "run")
}
