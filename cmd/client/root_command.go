package main

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"

	"go.openai.org/api/tunnel-client/pkg/config"
	"go.openai.org/api/tunnel-client/pkg/version"
)

func newRootCommand(lookupEnv func(string) (string, bool), stdout io.Writer, stderr io.Writer) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "tunnel-client",
		Short:         "Tunnel client for the OpenAI MCP control plane",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.SetOut(stdout)
	rootCmd.SetErr(stderr)

	config.RegisterFlags(rootCmd.PersistentFlags())

	writeUsage := func(cmd *cobra.Command) {
		writeRootUsage(rootCmd, cmd.OutOrStdout())
	}
	rootCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		writeUsage(cmd)
		return nil
	})
	rootCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		writeUsage(cmd)
	})
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error {
		writeUsage(cmd)
		return nil
	}
	rootCmd.Version = tunnelClientVersion()
	rootCmd.SetVersionTemplate("{{.Version}}\n")

	runCmd := &cobra.Command{
		Use:   "run",
		Short: "Run the tunnel client poller",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTunnel(cmd, lookupEnv)
		},
	}
	runCmd.SetUsageFunc(func(cmd *cobra.Command) error {
		config.WriteUsage(rootCmd.PersistentFlags(), cmd.OutOrStdout())
		return nil
	})
	runCmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		config.WriteUsage(rootCmd.PersistentFlags(), cmd.OutOrStdout())
	})
	rootCmd.AddCommand(runCmd)

	return rootCmd
}

func writeRootUsage(cmd *cobra.Command, w io.Writer) {
	config.WriteUsage(cmd.PersistentFlags(), w)

	availableCommands := cmd.Commands()
	if len(availableCommands) == 0 {
		return
	}

	_, _ = fmt.Fprintln(w, "\nCommands:")
	for _, subcmd := range availableCommands {
		if !subcmd.IsAvailableCommand() || subcmd.IsAdditionalHelpTopicCommand() {
			continue
		}
		if subcmd.Short != "" {
			_, _ = fmt.Fprintf(w, "  %s\t%s\n", subcmd.Name(), subcmd.Short)
		} else {
			_, _ = fmt.Fprintf(w, "  %s\n", subcmd.Name())
		}
	}
	_, _ = fmt.Fprintf(w, "\nUse \"%s [command] --help\" for more information about a command.\n", cmd.Name())
}

func tunnelClientVersion() string {
	if version.GitSHA != "" {
		return fmt.Sprintf("%s (git sha: %s)", version.Version, version.GitSHA)
	}
	return version.Version
}
