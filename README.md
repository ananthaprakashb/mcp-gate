# semantic-saga-mcp

An MCP server that gives autonomous agents **transactional compensation** for
multi-step side effects. The server owns the execution log, invokes configured
HTTP actions, and unwinds completed steps in reverse order when a tool fails or
the MCP client explicitly requests rollback.

The client never needs to know how to reverse a database write, payment, or
infrastructure operation. Compensation endpoints and their credentials remain
server-side.

## MCP tools

| Tool | Purpose |
| --- | --- |
| `saga_begin` | Start an isolated workflow and execution log. |
| `saga_execute` | Run a named action. A failure automatically compensates all prior steps. |
| `saga_commit` | Mark a successful workflow final. |
| `saga_rollback` | Explicitly compensate completed steps in reverse order. |
| `saga_get` | Inspect workflow status, results, and compensation attempts. |

Rollback is idempotent. A partially failed rollback has status
`rollback_failed`; calling `saga_rollback` again retries only uncompensated
steps. A committed saga cannot be changed or rolled back.

## Run

Requires Go 1.22+. Configure an allowlist of actions. Each action has a forward
and compensation endpoint:

```sh
export SAGA_ACTIONS='[
  {
    "name":"reserve_inventory",
    "execute_url":"http://inventory.internal/reservations",
    "compensate_url":"http://inventory.internal/reservations/release",
    "headers":{"Authorization":"Bearer server-side-secret"}
  },
  {
    "name":"charge_card",
    "execute_url":"http://payments.internal/charges",
    "compensate_url":"http://payments.internal/refunds"
  }
]'
go run ./cmd/semantic-saga-mcp
```

The Streamable HTTP MCP endpoint is `POST http://localhost:8080/mcp` (the
handler accepts POSTs on any path, so deployments may mount it under a custom
prefix). Set `SAGA_ADDR` to change the listen address.

Configure an MCP client with:

```json
{
  "mcpServers": {
    "semantic-saga": {
      "url": "http://localhost:8080/mcp"
    }
  }
}
```

## Action contract

Forward endpoints receive:

```json
{"saga_id":"checkout-42","step_id":1,"arguments":{"sku":"ABC"}}
```

They must return a JSON value on success. The result is retained in the log so
the compensation endpoint receives enough semantic context to undo the action:

```json
{
  "saga_id":"checkout-42",
  "step_id":1,
  "arguments":{"sku":"ABC"},
  "result":{"reservation_id":"r-123"}
}
```

Any non-2xx response, transport failure, oversized response, or invalid JSON is
an action failure. Upstream response bodies are not included in errors. Action
providers should use `(saga_id, step_id)` as an idempotency key because a
network failure cannot prove whether a remote mutation occurred. Compensation
handlers must also be idempotent.

## Guarantees and limits

- Actions and rollbacks are serialized, preventing a forward step from racing
  compensation within one server process.
- Only successful forward actions enter the log, and compensation runs in
  strict reverse order.
- Explicit and automatic rollback share the same retryable state machine.
- State is currently process-local. Restarting the server loses active logs;
  production deployments should add a durable store before relying on crash
  recovery.
- This implements the saga/compensating-transaction pattern, not ACID isolation
  across remote systems. An unreachable compensation service requires retry or
  operator intervention.

## Verify

```sh
go test -race ./...
go vet ./...
```
