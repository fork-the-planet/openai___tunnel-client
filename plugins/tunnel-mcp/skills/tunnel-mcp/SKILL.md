---
name: tunnel-mcp
description: Create, connect, list, and inspect MCP tunnel runtimes through the local tunnel-client plugin. Use when Codex needs to manage secure MCP tunnels with aliases and native tunnel-client runtime processes.
---

# Tunnel MCP

Use `scripts/tunnel_mcp` from this plugin when a user asks Codex to manage MCP
tunnels through `tunnel-client`. The plugin entrypoint is a thin router onto
the public native `tunnel-client runtimes ...` and
`tunnel-client admin-profiles ...` command trees.

Before acting, consult only the relevant curated reference file under
`references/`:

- `references/binary.md`: how to find or obtain a public-safe `tunnel-client` binary
- `references/setup-and-install.md`: install, export, reset, binary-vs-bundle setup
- `references/profiles-state-and-keys.md`: profiles, state dirs, admin/runtime key split
- `references/runtime-flows.md`: create, connect, list, status, stop, rm, attach by tunnel id
- `references/troubleshooting.md`: `/healthz`, `/readyz`, `/ui`, status, logs, stale aliases

Do not open every reference by default. Pick the smallest relevant set for the
prompt, then route action through native `tunnel-client` commands.
For create, connect, list, status, stop, rm, or attach-by-id prompts, open
`references/runtime-flows.md` first.

Binary setup order:

- first try `--tunnel-client-bin`, `TUNNEL_CLIENT_BIN`, installed
  `.tunnel-client-bin`, then adjacent build outputs
- treat ambient `PATH` candidates as diagnostics unless the user selected one
  with `TUNNEL_CLIENT_BIN` or `--tunnel-client-bin`
- if the binary is missing, consult `references/binary.md`
- do not auto-download, auto-clone, or run remote binaries unless the user
  explicitly asks Codex to install or set up `tunnel-client`

Missing-binary response contract: include the public-safe anchors
`https://github.com/openai/tunnel-client/releases/latest`,
`https://github.com/openai/tunnel-client`,
`git clone https://github.com/openai/tunnel-client.git`,
`go build -o bin/tunnel-client ./cmd/client`, Windows
`go build -o bin/tunnel-client.exe ./cmd/client`, `TUNNEL_CLIENT_BIN`, and
`--tunnel-client-bin /path/to/tunnel-client`.

Preferred install surfaces:

- `tunnel-client codex plugin install` when the binary is available
- `tunnel-client codex status` and `tunnel-client codex diagnose --json` for
  marketplace/cache/config/router diagnostics
- `tunnel-client codex plugin uninstall` when the installed plugin should be reset or removed
- `./plugins/tunnel-mcp/scripts/install_plugin.sh --tunnel-client-bin /path/to/tunnel-client` from a source checkout
- `sh scripts/install_plugin.sh --tunnel-client-bin /path/to/tunnel-client` from an exported plugin bundle root on macOS/Linux
- `powershell -NoProfile -ExecutionPolicy Bypass -File .\scripts\Install-Plugin.ps1 --tunnel-client-bin C:\path\to\tunnel-client.exe` from an exported plugin bundle root on Windows

## Rules

- Use `tunnel-client admin tunnels` for remote tunnel CRUD. Do not call raw
  tunnel-service HTTP endpoints from this plugin.
- Route operational actions through `tunnel-client runtimes ...` and
  `tunnel-client admin-profiles ...`.
- Use native `tunnel-client run --profile <name>`; do not translate profile
  files into flags in the plugin layer.
- Do not assume a source checkout, build system, helper, or tmux. The installed
  plugin must work with the selected `tunnel-client` binary alone.
- Tunnel state, admin profiles, generated runtime profiles, stale-alias
  handling, cleanup classification, and local process management are owned by
  native `tunnel-client`; consult the relevant reference before explaining those
  details.
- Keep admin and runtime credentials split: admin CRUD uses
  `admin-profiles`; runtime attach/connect uses `--runtime-api-key env:NAME` or
  `file:/path`. Do not pass literal keys.
- Never write literal API keys, bearer tokens, cookies, or inline `sk-` style
  secret material into plugin state or generated configs.
- Surface `control_plane_poll_health` separately from `/healthz` and `/readyz`;
  local readiness can be green while control-plane polling fails through a dead
  proxy.
