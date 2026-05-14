load("@bazel_skylib//lib:shell.bzl", "shell")
load("@rules_itest//:itest.bzl", "itest_service", "itest_task")

def _qualify_target(target):
    if target.startswith("@@//"):
        return target
    if target.startswith("//"):
        return "@@" + target
    if target.startswith(":"):
        return "@@//" + native.package_name() + target
    return "@@//" + native.package_name() + ":" + target

def _port_ref(target, port_name = None):
    qualified = _qualify_target(target)
    if port_name:
        qualified += ":" + port_name
    return "$${" + qualified + "}"

def _http_url(target, path = "", port_name = None):
    return "http://127.0.0.1:" + _port_ref(target, port_name) + path

def _runfile_path(workspace_name, file):
    return "{}/{}".format(workspace_name, file.short_path)

def _runfile_for_target(target):
    executable = target[DefaultInfo].files_to_run.executable
    if executable:
        return executable

    files = target[DefaultInfo].files.to_list()
    if len(files) != 1:
        fail("Target {} must provide exactly one file or an executable".format(target.label))
    return files[0]

def _profile_preflight_probe_binary_impl(ctx):
    script = ctx.actions.declare_file(ctx.label.name + ".sh")

    runfile_env_targets = {}
    for target, env_name in ctx.attr.runfile_env.items():
        if env_name in runfile_env_targets:
            fail("Duplicate runfile env name: {}".format(env_name))
        runfile_env_targets[env_name] = target

    lines = [
        "#!/usr/bin/env bash",
        "set -euo pipefail",
        "",
        "# --- begin runfiles.bash initialization ---",
        "set +e; f=bazel_tools/tools/bash/runfiles/runfiles.bash",
        'source "${RUNFILES_DIR:-/dev/null}/$f" 2>/dev/null || \\',
        '  source "$(grep -sm1 "^$f " "${RUNFILES_MANIFEST_FILE:-/dev/null}" | cut -f2- -d\' \')" 2>/dev/null || \\',
        '  source "$0.runfiles/$f" 2>/dev/null || \\',
        '  source "$(grep -sm1 "^$f " "$0.runfiles_manifest" | cut -f2- -d\' \')" 2>/dev/null || \\',
        '  source "$(grep -sm1 "^$f " "$0.exe.runfiles_manifest" | cut -f2- -d\' \')" 2>/dev/null || \\',
        '  { echo >&2 "ERROR: cannot find $f"; exit 1; }; f=; set -e',
        "# --- end runfiles.bash initialization ---",
        "",
    ]

    for env_name in sorted(runfile_env_targets):
        lines.append('export {}="$(rlocation {})"'.format(
            env_name,
            shell.quote(_runfile_path(ctx.workspace_name, _runfile_for_target(runfile_env_targets[env_name]))),
        ))

    lines.extend([
        "",
        'exec "$(rlocation {})" \\'.format(shell.quote(_runfile_path(ctx.workspace_name, ctx.executable._runtime_helper))),
        '  "$(rlocation {})" \\'.format(shell.quote(_runfile_path(ctx.workspace_name, ctx.executable.client))),
        '  "$(rlocation {})" \\'.format(shell.quote(_runfile_path(ctx.workspace_name, ctx.file.profile))),
        '  "$@"',
        "",
    ])

    ctx.actions.write(script, "\n".join(lines), is_executable = True)

    runfiles = ctx.runfiles(files = [ctx.file.profile]).merge(
        ctx.attr._bash_runfiles[DefaultInfo].default_runfiles,
    ).merge(
        ctx.attr.client[DefaultInfo].default_runfiles,
    ).merge(
        ctx.attr._runtime_helper[DefaultInfo].default_runfiles,
    )
    for env_name in sorted(runfile_env_targets):
        runfiles = runfiles.merge(runfile_env_targets[env_name][DefaultInfo].default_runfiles)

    return [DefaultInfo(
        executable = script,
        files = depset([script]),
        runfiles = runfiles,
    )]

_profile_preflight_probe_binary = rule(
    implementation = _profile_preflight_probe_binary_impl,
    executable = True,
    attrs = {
        "client": attr.label(
            default = "//api/tunnel-client/cmd/client:client",
            cfg = "exec",
            executable = True,
        ),
        "profile": attr.label(
            allow_single_file = True,
            mandatory = True,
        ),
        "runfile_env": attr.label_keyed_string_dict(),
        "_bash_runfiles": attr.label(
            default = "@bazel_tools//tools/bash/runfiles",
        ),
        "_runtime_helper": attr.label(
            default = "//api/tunnel-client:tunnel_client_profile_preflight_probe_runtime",
            cfg = "exec",
            executable = True,
        ),
    },
)

def _profile_wrapper_args(profile, profile_env = None, runfile_env = None):
    args = [
        "--client-path",
        "$(rlocationpath //api/tunnel-client/cmd/client:client)",
        "--profile-file",
        "$(rlocationpath " + profile + ")",
    ]
    args.extend(_env_args(profile_env))
    args.extend(_runfile_env_args(runfile_env))
    args.append("--")
    return args

def _env_args(profile_env = None):
    args = []
    for env_name, value in sorted((profile_env or {}).items()):
        args.extend([
            "--set-env",
            env_name,
            value,
        ])
    return args

def _runfile_env_args(runfile_env = None):
    args = []
    for env_name, target in sorted((runfile_env or {}).items()):
        args.extend([
            "--runfile-env",
            env_name + "=" + "$(rlocationpath " + target + ")",
        ])
    return args

def _runtime_env(control_plane_base_url, extra_env = None):
    runtime_env = {
        "CONTROL_PLANE_BASE_URL": control_plane_base_url,
    }
    if extra_env:
        runtime_env.update(extra_env)
    return runtime_env

def tunnel_client_profile_itest_service(
        name,
        profile,
        control_plane_base_url,
        extra_env = None,
        runfile_env = None,
        data = None,
        deps = None,
        expected_start_duration = "5s",
        health_check_timeout = "90s",
        tags = None):
    service_data = [
        "//api/tunnel-client/cmd/client",
        profile,
    ] + list(data or [])
    service_data.extend((runfile_env or {}).values())

    runtime_env = _runtime_env(
        control_plane_base_url = control_plane_base_url,
        extra_env = extra_env,
    )
    runtime_env["HEALTH_LISTEN_ADDR"] = "127.0.0.1:" + _port_ref(name, "health_port")

    itest_service(
        name = name,
        args = _profile_wrapper_args(profile, profile_env = runtime_env, runfile_env = runfile_env),
        data = service_data,
        deps = list(deps or []),
        exe = "//api/tunnel-client:run_tunnel_client_profile_itest_service",
        expected_start_duration = expected_start_duration,
        health_check_timeout = health_check_timeout,
        http_health_check_address = _http_url(name, "/readyz", "health_port"),
        named_ports = ["health_port"],
        tags = list(tags or []) + [
            "manual",
            "no-remote-exec",
        ],
    )

def tunnel_client_profile_itest_task(
        name,
        profile,
        preflight_base_url,
        control_plane_base_url,
        extra_env = None,
        runfile_env = None,
        data = None,
        deps = None,
        tags = None):
    runtime_env = _runtime_env(
        control_plane_base_url = control_plane_base_url,
        extra_env = extra_env,
    )
    runtime_env["HEALTH_LISTEN_ADDR"] = "127.0.0.1:0"

    probe_binary_name = name + "_preflight_probe"
    probe_runfile_env = {}
    for env_name, target in (runfile_env or {}).items():
        probe_runfile_env[target] = env_name

    _profile_preflight_probe_binary(
        name = probe_binary_name,
        profile = profile,
        runfile_env = probe_runfile_env,
        tags = ["manual"],
        visibility = ["//visibility:private"],
    )

    itest_task(
        name = name,
        args = [preflight_base_url],
        data = list(data or []),
        deps = list(deps or []),
        env = runtime_env,
        exe = ":" + probe_binary_name,
        tags = list(tags or []) + [
            "manual",
            "no-remote-exec",
        ],
    )
