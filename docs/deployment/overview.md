# Deployment Overview

This section covers common ways to run `tunnel-client` and the network requirements it needs.

## Required egress

Your environment must allow `tunnel-client` to make **outbound HTTPS** connections to:

- **Host**: `api.openai.com`
- **Port**: `443/TCP`
- **Paths**: `/v1/tunnel/*`

No inbound ports are required for the tunnel itself.

`tunnel-client` must also be able to reach your internal MCP server at the configured `MCP_SERVER_URL`.

## Choose a deployment pattern

- **Docker**: [`docker.md`](docker.md)
- **Kubernetes sidecar**: [`kubernetes-sidecar.md`](kubernetes-sidecar.md)
- **Kubernetes dedicated pod**: [`kubernetes-dedicated.md`](kubernetes-dedicated.md)
- **VM / systemd**: [`systemd-vm.md`](systemd-vm.md)
