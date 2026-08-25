# Contributing to semantic-saga-mcp

## Development

Requires Go 1.22 or newer.

```sh
git clone https://github.com/ananthaprakashb/semantic-saga-mcp.git
cd semantic-saga-mcp
go test -race ./...
go vet ./...
```

Run `gofmt` on changed Go files. Changes to saga state transitions must include
successful, failure, retry, and invalid-transition tests where applicable.
Changes to the MCP surface must preserve JSON-RPC error semantics and include a
protocol-level test.

## Design requirements

- Never ask an MCP client to construct a compensating request.
- Preserve reverse-order compensation and do not compensate a step twice.
- Treat action and compensation calls as at-least-once operations; remote
  handlers must use `(saga_id, step_id)` for idempotency.
- Do not describe process-local state as durable or crash-safe.
- Do not expose upstream response bodies or server-side credentials.
- Avoid adding dependencies when the standard library is sufficient.

Pull requests should explain compatibility, transaction-state, security, and
rollout implications. Never commit real credentials or private endpoints.
