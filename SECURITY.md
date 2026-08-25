# Security Policy

`semantic-saga-mcp` coordinates side effects and therefore belongs inside a
trusted service boundary. Report vulnerabilities privately through GitHub's
security advisory form rather than a public issue.

## Trust model

The MCP server, its action configuration, and configured action services are
trusted. MCP clients and tool arguments are untrusted. Credentials in
`SAGA_ACTIONS` remain server-side and must be supplied through an appropriate
secret-management mechanism in production.

The current server does not implement client authentication or TLS. Deploy it
behind an authenticated, rate-limited TLS ingress, restrict its network access
to the intended action services, and prevent untrusted users from changing
`SAGA_ACTIONS`.

## Transaction guarantees

This project implements compensating transactions, not distributed ACID
transactions. A timeout cannot establish whether a remote side effect occurred.
Action services must consequently treat `(saga_id, step_id)` as an idempotency
key, and compensation operations must be idempotent.

Execution logs are process-local. A restart loses them, so the current release
does not provide crash recovery. Operators must not claim durable rollback until
a persistent store is configured. A `rollback_failed` state requires retry or
operator intervention.

## Sensitive-data handling

- Do not put secrets in MCP tool arguments, action results, or saga IDs.
- Do not return sensitive values from action endpoints; successful results are
  retained in the in-memory execution log and returned by `saga_get`.
- Upstream non-success bodies are discarded, but status codes are exposed.
- Configure strict egress controls because action URLs and headers are trusted
  server configuration.
- Keep request and response size limits enabled.

Security fixes are applied to the default branch until versioned support
policies are published.
