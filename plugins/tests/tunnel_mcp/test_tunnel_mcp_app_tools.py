from __future__ import annotations

import json
import os
import pathlib
import shutil
import stat
import subprocess
import tempfile
import textwrap
import unittest

PLUGIN_ROOT = pathlib.Path(__file__).resolve().parents[2] / "tunnel-mcp"
BIN_HINT = PLUGIN_ROOT / ".tunnel-client-bin"


def _node_binary() -> str:
    node = os.environ.get("NODE_BINARY") or shutil.which("node")
    if not node:
        raise unittest.SkipTest("node binary is not available in this test environment")
    return node


def _write_fake_tunnel_client(path: pathlib.Path) -> None:
    path.write_text(
        textwrap.dedent(
            """\
            #!/bin/sh
            printf '%s\n' "$@" > "$TUNNEL_MCP_APP_TEST_ARGS"
            case "$1 $2" in
              "runtimes create")
                cat <<'JSON'
{
  "alias": "docs-mcp",
  "tunnel": {
    "id": "tunnel_create"
  },
  "profile_path": "/tmp/docs-create.yaml",
  "repair_actions": []
}
JSON
                ;;
              "runtimes connect")
                printf '%s\n' 'native diagnostic before json'
                cat <<'JSON'
{
  "alias": "docs-mcp",
  "tunnel": {
    "id": "tunnel_connect"
  },
  "profile_path": "/tmp/docs-connect.yaml",
  "session_name": "tunnel-mcp__docs-mcp",
  "local": {
    "effective_health": {
      "healthz": {
        "ok": true,
        "status": 200,
        "url": "http://127.0.0.1:18080/healthz"
      },
      "readyz": {
        "ok": true,
        "status": 200,
        "url": "http://127.0.0.1:18080/readyz"
      }
    },
    "control_plane_poll_health": {
      "state": "ok"
    }
  },
  "repair_actions": []
}
JSON
                ;;
              "runtimes status")
                cat <<'JSON'
{
  "alias": "docs-mcp",
  "tunnel_id": "tunnel_status",
  "profile_path": "/tmp/docs-status.yaml",
  "session_name": "tunnel-mcp__docs-mcp",
  "healthz": {
    "ok": true,
    "status": 200
  },
  "readyz": {
    "ok": true,
    "status": 200
  },
  "control_plane_poll_health": {
    "state": "ok"
  },
  "repair_actions": [
    {
      "action": "none",
      "reason": "healthy"
    }
  ]
}
JSON
                ;;
              "runtimes list")
                cat <<'JSON'
{
  "state_root": "/tmp/tunnel-mcp",
  "aliases": [
    {
      "alias": "docs-mcp",
      "tunnel_id": "tunnel_status",
      "runtime_state": "ready"
    }
  ]
}
JSON
                ;;
              "runtimes stop")
                printf '%s\n' '{"alias":"docs-mcp","tunnel_id":"tunnel_status","stopped":true,"repair_actions":[]}'
                ;;
            esac
            """
        ),
        encoding="utf-8",
    )
    path.chmod(path.stat().st_mode | stat.S_IXUSR)


def _node_call(name: str, args: dict, env: dict[str, str]) -> dict:
    script = """
const server = require("./mcp/server.cjs");
server.callTool(process.argv[1], JSON.parse(process.argv[2]))
  .then((result) => process.stdout.write(JSON.stringify(result)))
  .catch((error) => {
    process.stderr.write(error && error.message ? error.message : String(error));
    process.exit(1);
  });
"""
    result = subprocess.run(
        [_node_binary(), "-e", script, name, json.dumps(args)],
        cwd=PLUGIN_ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )
    if result.returncode != 0:
        raise AssertionError(result.stderr or result.stdout)
    return json.loads(result.stdout)


def _node_call_error(name: str, args: dict, env: dict[str, str]) -> str:
    script = """
const server = require("./mcp/server.cjs");
server.callTool(process.argv[1], JSON.parse(process.argv[2]))
  .then(() => process.exit(2))
  .catch((error) => process.stdout.write(error && error.message ? error.message : String(error)));
"""
    result = subprocess.run(
        [_node_binary(), "-e", script, name, json.dumps(args)],
        cwd=PLUGIN_ROOT,
        env=env,
        text=True,
        capture_output=True,
        check=True,
    )
    return result.stdout


class TunnelMCPAppToolsTest(unittest.TestCase):
    def test_tool_definitions_expose_durable_runtime_jobs(self) -> None:
        script = """
const server = require("./mcp/server.cjs");
process.stdout.write(JSON.stringify(server.toolDefinitions().map((tool) => tool.name)));
"""
        result = subprocess.run(
            [_node_binary(), "-e", script],
            cwd=PLUGIN_ROOT,
            text=True,
            capture_output=True,
            check=True,
        )
        self.assertEqual(
            json.loads(result.stdout),
            [
                "install_or_select_tunnel_client",
                "create_tunnel_runtime",
                "connect_stdio_mcp",
                "list_runtime_aliases",
                "runtime_status",
                "stop_runtime",
            ],
        )

    def test_connect_stdio_mcp_invokes_native_runtime_and_normalizes_output(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            fake_bin = tmp_path / "tunnel-client"
            args_path = tmp_path / "args.txt"
            _write_fake_tunnel_client(fake_bin)

            result = _node_call(
                "connect_stdio_mcp",
                {
                    "alias": "docs-mcp",
                    "organization_id": "org_123",
                    "mcp_command": "python /tmp/server.py",
                    "tunnel_client_bin": str(fake_bin),
                },
                {**os.environ, "TUNNEL_MCP_APP_TEST_ARGS": str(args_path)},
            )

            payload = result["structuredContent"]
            self.assertEqual(payload["alias"], "docs-mcp")
            self.assertEqual(payload["tunnel_id"], "tunnel_connect")
            self.assertEqual(payload["profile_path"], "/tmp/docs-connect.yaml")
            self.assertEqual(payload["session_name"], "tunnel-mcp__docs-mcp")
            self.assertEqual(payload["healthz"]["status"], 200)
            self.assertEqual(payload["readyz"]["status"], 200)
            self.assertEqual(payload["control_plane_poll_health"]["state"], "ok")
            self.assertEqual(payload["repair_actions"], [])
            self.assertEqual(payload["selected_tunnel_client_bin"], str(fake_bin))
            self.assertEqual(
                args_path.read_text(encoding="utf-8").splitlines(),
                [
                    "runtimes",
                    "connect",
                    "--alias",
                    "docs-mcp",
                    "--mcp-command",
                    "python /tmp/server.py",
                    "--organization-id",
                    "org_123",
                    "--json",
                ],
            )

    def test_runtime_status_preserves_repair_actions_and_required_fields(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            fake_bin = tmp_path / "tunnel-client"
            _write_fake_tunnel_client(fake_bin)

            result = _node_call(
                "runtime_status",
                {"alias": "docs-mcp", "tunnel_client_bin": str(fake_bin)},
                {**os.environ, "TUNNEL_MCP_APP_TEST_ARGS": str(tmp_path / "args.txt")},
            )

            payload = result["structuredContent"]
            for key in [
                "tunnel_id",
                "alias",
                "profile_path",
                "healthz",
                "readyz",
                "control_plane_poll_health",
                "session_name",
                "repair_actions",
            ]:
                self.assertIn(key, payload)
            self.assertEqual(payload["repair_actions"][0]["action"], "none")
            self.assertEqual(payload["live_process_command"], None)
            self.assertEqual(payload["live_process_binary"], None)

    def test_list_runtime_aliases_invokes_native_runtime_list(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            fake_bin = tmp_path / "tunnel-client"
            args_path = tmp_path / "args.txt"
            _write_fake_tunnel_client(fake_bin)

            result = _node_call(
                "list_runtime_aliases",
                {"organization_id": "org_123", "tunnel_client_bin": str(fake_bin)},
                {**os.environ, "TUNNEL_MCP_APP_TEST_ARGS": str(args_path)},
            )

            payload = result["structuredContent"]
            self.assertEqual(payload["native"]["aliases"][0]["alias"], "docs-mcp")
            self.assertEqual(
                args_path.read_text(encoding="utf-8").splitlines(),
                ["runtimes", "list", "--organization-id", "org_123", "--json"],
            )

    def test_runtime_status_reports_live_process_binary_and_launch_diagnostics(self) -> None:
        script = """
const server = require("./mcp/server.cjs");
const payload = server.normalizedPayload(
  {operation: "runtime_status", tunnel_client_bin: "/selected/tunnel-client"},
  {
    process: {command: "'/tmp/tunnel client' run --profile docs-mcp"},
    local: {log: {path: "/tmp/runtime.log", tail: "boom"}}
  },
);
process.stdout.write(JSON.stringify(payload));
"""
        result = subprocess.run(
            [_node_binary(), "-e", script],
            cwd=PLUGIN_ROOT,
            text=True,
            capture_output=True,
            check=True,
        )
        payload = json.loads(result.stdout)
        self.assertEqual(payload["selected_tunnel_client_bin"], "/selected/tunnel-client")
        self.assertEqual(
            payload["live_process_command"], "'/tmp/tunnel client' run --profile docs-mcp"
        )
        self.assertEqual(payload["live_process_binary"], "/tmp/tunnel client")
        self.assertEqual(payload["launch_diagnostics"]["log_tail"], "boom")

    def test_argument_validation_rejects_bad_connect_requests(self) -> None:
        error = _node_call_error(
            "connect_stdio_mcp",
            {
                "alias": "bad alias",
                "organization_id": "org_123",
                "mcp_command": "python /tmp/server.py",
            },
            dict(os.environ),
        )
        self.assertIn("alias must start with a letter or number", error)

        error = _node_call_error(
            "connect_stdio_mcp",
            {
                "alias": "docs-mcp",
                "organization_id": "org_123",
                "workspace_id": "ws_123",
                "mcp_command": "python /tmp/server.py",
            },
            dict(os.environ),
        )
        self.assertIn("requires exactly one of organization_id, workspace_id, or tunnel_id", error)

        error = _node_call_error(
            "connect_stdio_mcp",
            {
                "alias": "docs-mcp",
                "organization_id": "org_123",
                "mcp_command": "python /tmp/server.py",
                "runtime_api_key": "sk-literal",
            },
            dict(os.environ),
        )
        self.assertIn("runtime_api_key must be a secret reference", error)

    def test_install_or_select_tunnel_client_returns_structured_selection(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            fake_bin = pathlib.Path(tmp) / "tunnel-client"
            _write_fake_tunnel_client(fake_bin)

            result = _node_call(
                "install_or_select_tunnel_client",
                {
                    "tunnel_client_bin": str(fake_bin),
                    "persist_hint": False,
                },
                dict(os.environ),
            )

            payload = result["structuredContent"]
            self.assertTrue(payload["ok"])
            self.assertEqual(payload["selection_source"], "explicit")
            self.assertEqual(payload["tunnel_client_bin"], str(fake_bin))
            self.assertIsNone(payload["tunnel_id"])
            self.assertIsNone(payload["healthz"])
            self.assertEqual(payload["repair_actions"], [])

    def test_install_or_select_tunnel_client_persists_env_selection_by_default(self) -> None:
        old_hint = BIN_HINT.read_text(encoding="utf-8") if BIN_HINT.exists() else None
        try:
            if BIN_HINT.exists():
                BIN_HINT.unlink()
            with tempfile.TemporaryDirectory() as tmp:
                tmp_path = pathlib.Path(tmp)
                fake_bin = tmp_path / "tunnel-client"
                _write_fake_tunnel_client(fake_bin)

                result = _node_call(
                    "install_or_select_tunnel_client",
                    {"allow_path_lookup": True},
                    {
                        **os.environ,
                        "TUNNEL_CLIENT_BIN": str(fake_bin),
                        "TUNNEL_MCP_APP_TEST_ARGS": str(tmp_path / "args.txt"),
                    },
                )

                payload = result["structuredContent"]
                self.assertTrue(payload["ok"])
                self.assertEqual(payload["selection_source"], "TUNNEL_CLIENT_BIN")
                self.assertEqual(BIN_HINT.read_text(encoding="utf-8").strip(), str(fake_bin))
        finally:
            if old_hint is None:
                try:
                    BIN_HINT.unlink()
                except FileNotFoundError:
                    pass
            else:
                BIN_HINT.write_text(old_hint, encoding="utf-8")


if __name__ == "__main__":
    unittest.main()
