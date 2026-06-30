# Tunnel client wire protocol

This document is for authors of tunnel clients in any language. It describes
the HTTP methods, headers, JSON shapes, and lifecycle used between a client and
the Secure MCP Tunnel control plane.

The machine-readable contract is [`openapi.json`](openapi.json). Use it to
generate types or validate fixtures, and use this document for behavior that
OpenAPI alone cannot express.

## Scope

A tunnel client:

1. authenticates to `https://api.openai.com`;
2. optionally fetches tunnel metadata for startup diagnostics;
3. long-polls for commands addressed to one tunnel;
4. forwards each command to the configured MCP server; and
5. posts the MCP result back to the control plane.

The canonical client endpoints are:

| Method | Path | Purpose |
| --- | --- | --- |
| `GET` | `/v1/tunnels/{tunnel_id}` | Fetch minimal startup metadata. |
| `GET` | `/v1/tunnels/{tunnel_id}/poll` | Long-poll for pending commands. |
| `POST` | `/v1/tunnels/{tunnel_id}/response` | Return the result for one command. |

Use the plural `/v1/tunnels/...` paths. Singular `/v1/tunnel/...` paths are
compatibility aliases and are not part of the contract for new clients.

## Authentication and common headers

Send the tunnel API key on every request:

```http
Authorization: Bearer <tunnel-api-key>
```

Clients should also send a stable implementation name and version. These
headers are diagnostic metadata, not feature negotiation:

```http
X-Tunnel-Client-Name: example-rust-client
X-Tunnel-Client-Version: 1.2.3
```

Treat tunnel IDs, request IDs, and shard tokens as opaque strings. Do not parse
them or infer routing from their contents.

## Poll loop

Request:

```http
GET /v1/tunnels/tunnel_123/poll?limit=25&timeout_ms=15000 HTTP/1.1
Authorization: Bearer <tunnel-api-key>
X-Tunnel-Client-Name: example-rust-client
X-Tunnel-Client-Version: 1.2.3
```

`limit` is optional and must be from `1` through `25`. It is a request hint:
if a successful response contains more commands than requested, process every
command; do not drop the excess. `timeout_ms` is an optional requested
long-poll wait in milliseconds. The service bounds the effective wait, so a
client must not assume the requested duration is exact.

A `204 No Content` response means the poll completed without commands. Issue
another poll. A `200 OK` response contains a JSON envelope:

```json
{
  "commands": [
    {
      "request_id": "req_123",
      "shard_token": "opaque-shard-token",
      "command_type": "jsonrpc",
      "channel": "main",
      "created_at": "2026-01-01T00:00:00Z",
      "headers": {
        "Mcp-Session-Id": ["session_123"]
      },
      "jsonrpc": {
        "jsonrpc": "2.0",
        "id": "rpc_123",
        "method": "tools/list",
        "params": {}
      }
    }
  ]
}
```

Common command fields:

| Field | Meaning |
| --- | --- |
| `request_id` | Opaque correlation ID. Echo it as `request_id` in the response body. |
| `shard_token` | Opaque routing token. Echo it only in `X-Tunnel-Shard-Token` when posting the response. |
| `command_type` | Discriminator for the command shape. |
| `channel` | Logical MCP channel; defaults to `main` when absent. Echo it in the response body. |
| `created_at` | RFC 3339 enqueue timestamp. |
| `headers` | Multi-valued headers to apply to the MCP request. |

### `jsonrpc` commands

For `command_type: "jsonrpc"`, `jsonrpc` is the raw JSON-RPC request or
notification to send to the MCP server. Preserve JSON-RPC IDs and do not
reinterpret the payload as a tunnel-protocol object.

### `session_termination` commands

For `command_type: "session_termination"`, close the Streamable HTTP session
identified by the `Mcp-Session-Id` header. The command has no `jsonrpc` field.
After closing the session, post a response with
`resp_type: "session_termination_response"`, typically `resp_code: 204`, and
no `resp_json`.

### Future command types

Dispatch on `command_type`, not on field presence. If a client receives an
unknown command type, it must not reinterpret it as JSON-RPC. Log the
unsupported discriminator with the opaque `request_id`, continue serving
known commands, and keep polling.

## Posting a response

Every response POST must include the `shard_token` from the polled command:

```http
POST /v1/tunnels/tunnel_123/response HTTP/1.1
Authorization: Bearer <tunnel-api-key>
Content-Type: application/json
X-Tunnel-Shard-Token: opaque-shard-token
```

The shard token belongs in the HTTP header only; never put it in the JSON
body. `X-Client-Request-Id` is optional diagnostic correlation when the client
has one.

JSON-RPC result example:

```json
{
  "request_id": "req_123",
  "channel": "main",
  "resp_json": {
    "jsonrpc": "2.0",
    "id": "rpc_123",
    "result": {
      "tools": []
    }
  },
  "resp_headers": {
    "Content-Type": ["application/json"]
  },
  "resp_code": 200,
  "resp_type": "jsonrpc_response"
}
```

Response fields:

| Field | Required | Meaning |
| --- | --- | --- |
| `request_id` | yes | The polled command's opaque request ID. |
| `channel` | no | Logical channel; send the command's channel when present. |
| `resp_json` | depends | JSON-RPC payload; omit for acknowledgment-only responses. |
| `resp_headers` | no | Multi-valued upstream MCP response headers. |
| `resp_code` | yes | HTTP-style status code from the MCP interaction. |
| `resp_type` | no | Payload discriminator; defaults to `jsonrpc_response`. |

Supported `resp_type` values:

| Value | Use |
| --- | --- |
| `jsonrpc_response` | Final JSON-RPC result or error with `resp_json`. |
| `jsonrpc_notify` | Non-final JSON-RPC notification with `resp_json`. |
| `notify_ack` | Acknowledgment for a notification that has no JSON-RPC result. |
| `session_termination_response` | Acknowledgment after closing an MCP session. |

A successful POST returns:

```json
{
  "status": "ok"
}
```

## Errors, retries, and concurrency

- Keep polling until the process is stopped; `204` is normal, not an error.
- Retry transient network failures, `429`, and `5xx` with bounded backoff.
- Treat `401` and `403` as authentication or authorization failures that need
  operator action instead of a tight retry loop.
- A response POST can return `404` when the request has already been fulfilled
  or is no longer pending. Treat that command as terminal and do not replay
  the MCP operation.
- A client may process multiple commands concurrently, but correlation is
  always per command: pair each `request_id`, `channel`, and `shard_token` from
  one poll item with that item's response.
- Preserve multi-valued headers. Do not collapse repeated values into a
  comma-separated string unless the MCP transport itself requires it.

## Language-neutral implementation sketch

```text
loop:
  poll = GET /v1/tunnels/{tunnel_id}/poll?limit=25&timeout_ms=15000
  if poll.status == 204:
    continue
  if poll.status != 200:
    handle_control_plane_error(poll)
    continue

  for command in poll.body.commands:
    result = dispatch_by_command_type(command)
    POST /v1/tunnels/{tunnel_id}/response
      header X-Tunnel-Shard-Token = command.shard_token
      body.request_id = command.request_id
      body.channel = command.channel
      body.resp_* = result
```

## Implementation checklist

- Generate or hand-write models from [`openapi.json`](openapi.json).
- Send bearer auth and stable client name/version headers.
- Use only the canonical plural endpoints.
- Handle `200` and `204` poll responses.
- Support both documented `command_type` values.
- Preserve raw JSON-RPC payloads and multi-valued headers.
- Echo `request_id`, `channel`, and `shard_token` in the correct locations.
- Cover each response discriminator with fixtures.
- Ignore unknown JSON fields for forward compatibility.
- Validate fixtures against the OpenAPI document in CI.
