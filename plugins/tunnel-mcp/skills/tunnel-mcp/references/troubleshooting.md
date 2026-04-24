# Troubleshooting

Start with local runtime state, not remote guesses:

- `tunnel-client runtimes status <alias>`
- `tunnel-client doctor --profile <name>`
- `tunnel-client doctor --profile <name> --explain`

Health/debug signals:

- `health_url_file`: the file that stores the resolved local health base URL
- `/healthz`: liveness
- `/readyz`: readiness
- `/ui`: local admin UI
- `ui_url`: the explicit admin UI URL exposed in runtime status

If `tmux` is available, tunnel-client prefers a tmux-managed runtime. When
`tmux` is unavailable, it falls back to a detached background process and
records PID/log path in local state.

If `readyz` is failing:

- confirm the runtime process is still alive
- read the runtime log path from `runtimes status`
- confirm the runtime key/admin profile split is correct
- verify the generated profile path and referenced secrets are the ones you expect

If a stored alias points at a missing remote tunnel, `create` and `connect`
treat it as recoverable and continue with scoped lookup or creation, while
`status` reports the stale alias instead of silently creating a replacement.
