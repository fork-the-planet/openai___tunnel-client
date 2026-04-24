package pluginsbundle

import (
	assistantkb "go.openai.org/api/tunnel-client/docs"
)

const (
	tunnelMCPPromptMatchLimit   = 2
	tunnelMCPPromptExcerptChars = 700
)

var tunnelMCPReferenceFiles = []string{
	"tunnel-mcp/skills/tunnel-mcp/references/binary.md",
	"tunnel-mcp/skills/tunnel-mcp/references/setup-and-install.md",
	"tunnel-mcp/skills/tunnel-mcp/references/profiles-state-and-keys.md",
	"tunnel-mcp/skills/tunnel-mcp/references/runtime-flows.md",
	"tunnel-mcp/skills/tunnel-mcp/references/troubleshooting.md",
}

func BuildTunnelMCPPromptContext(prompt string) string {
	matches := assistantkb.SearchFS(
		prompt,
		embeddedPluginFiles,
		tunnelMCPReferenceFiles,
		"plugins/",
		tunnelMCPPromptMatchLimit,
		tunnelMCPPromptExcerptChars,
	)
	return assistantkb.FormatPromptContext([]string{
		"Curated tunnel-mcp plugin references injected from the binary.",
		"These snippets cover binary acquisition, plugin setup, runtime flows, profiles, state dirs, key split, and troubleshooting.",
		"Use them before guessing how the Codex plugin should create, connect, inspect, or debug a tunnel runtime.",
	}, "plugin_knowledge.match", matches)
}
