from __future__ import annotations

import os
import pathlib
import stat
import subprocess
import tempfile
import unittest

PLUGIN_ROOT = pathlib.Path(__file__).resolve().parents[1]
ENTRYPOINT = PLUGIN_ROOT / "scripts" / "tunnel_mcp"


def _write_fake_tunnel_client(path: pathlib.Path) -> None:
    path.write_text(
        "#!/usr/bin/env python3\n"
        "import json, os, pathlib, sys\n"
        "pathlib.Path(os.environ['ROUTER_TEST_OUTPUT']).write_text(json.dumps(sys.argv[1:]))\n",
        encoding="utf-8",
    )
    path.chmod(path.stat().st_mode | stat.S_IXUSR)


class TunnelMCPRouterTest(unittest.TestCase):
    def test_router_forwards_sessions_command_and_adds_json(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            output_path = tmp_path / "args.json"
            fake_bin = tmp_path / "tunnel-client"
            _write_fake_tunnel_client(fake_bin)

            result = subprocess.run(
                [
                    str(ENTRYPOINT),
                    "--tunnel-client-bin",
                    str(fake_bin),
                    "connect",
                    "--alias",
                    "docs-mcp",
                    "--mcp-command",
                    "python server.py",
                ],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "ROUTER_TEST_OUTPUT": str(output_path)},
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                output_path.read_text(encoding="utf-8"),
                '["sessions", "connect", "--alias", "docs-mcp", "--mcp-command", '
                '"python server.py", "--json"]',
            )

    def test_router_forwards_admin_profiles_command_and_adds_json(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            output_path = tmp_path / "args.json"
            fake_bin = tmp_path / "tunnel-client"
            _write_fake_tunnel_client(fake_bin)

            result = subprocess.run(
                [
                    str(ENTRYPOINT),
                    "--tunnel-client-bin",
                    str(fake_bin),
                    "admin-profiles",
                    "list",
                ],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "ROUTER_TEST_OUTPUT": str(output_path)},
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                output_path.read_text(encoding="utf-8"),
                '["admin-profiles", "list", "--json"]',
            )

    def test_router_forwards_rm_alias_and_adds_json(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            tmp_path = pathlib.Path(tmp)
            output_path = tmp_path / "args.json"
            fake_bin = tmp_path / "tunnel-client"
            _write_fake_tunnel_client(fake_bin)

            result = subprocess.run(
                [
                    str(ENTRYPOINT),
                    "--tunnel-client-bin",
                    str(fake_bin),
                    "rm",
                    "docs-mcp",
                ],
                check=False,
                capture_output=True,
                text=True,
                env={**os.environ, "ROUTER_TEST_OUTPUT": str(output_path)},
            )

            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(
                output_path.read_text(encoding="utf-8"),
                '["sessions", "rm", "docs-mcp", "--json"]',
            )

    def test_router_help_lists_supported_routes(self) -> None:
        result = subprocess.run(
            [str(ENTRYPOINT), "--help"],
            check=False,
            capture_output=True,
            text=True,
            env=os.environ,
        )

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("admin-profiles", result.stdout)
        self.assertIn("rm", result.stdout)


if __name__ == "__main__":
    unittest.main()
