# Contributing to mcp-gate

Thanks for your interest in improving `mcp-gate`.

The project is intentionally small and security-sensitive. Contributions that improve correctness, auditability, interoperability, documentation, testing, or operational safety are especially welcome.

Before changing authorization behavior, token semantics, replay handling, schema validation, or credential forwarding, please read [SECURITY.md](SECURITY.md).

## Ways to contribute

Useful contributions include:

- bug fixes;
- security hardening;
- threat-model review;
- bypass and negative tests;
- documentation improvements;
- MCP integration examples;
- replay-store implementations;
- observability improvements;
- deployment examples;
- performance improvements with clear benchmarks;
- portability fixes;
- narrowly scoped feature proposals.

For substantial new behavior, opening an issue first is encouraged so the design can be discussed before a large implementation is written.

## Development requirements

- Go 1.22 or newer;
- Git;
- Docker and Docker Compose for the end-to-end example;
- `govulncheck` for dependency/toolchain vulnerability checks.

Clone the repository:

```sh
git clone https://github.com/ananthaprakashb/mcp-gate.git
cd mcp-gate
```

Run the test suite:

```sh
go test ./...
```

Run the race detector:

```sh
go test -race ./...
```

Run static checks:

```sh
go vet ./...
```

Run the vulnerability scanner:

```sh
go run golang.org/x/vuln/cmd/govulncheck@latest ./...
```

Run the end-to-end demo:

```sh
cd examples
docker compose up --build --abort-on-container-exit --exit-code-from agent
```

The GitHub Actions workflow runs the race-enabled tests, `go vet`, and `govulncheck` on pushes and pull requests.

## Keep changes focused

Small, reviewable pull requests are preferred.

A good pull request should ideally do one thing well. Avoid mixing unrelated refactoring, formatting, dependency changes, and security behavior changes in the same PR unless they are inseparable.

This is especially important for authorization code because small diffs are easier to reason about and audit.

## Coding style

Use normal Go conventions.

Before submitting:

```sh
gofmt -w .
go test -race ./...
go vet ./...
```

Prefer straightforward standard-library code where practical. New dependencies should provide clear value because dependency size and transitive behavior matter in security-sensitive infrastructure.

Avoid clever abstractions that make authorization flow harder to inspect.

## Security-sensitive contributions

Changes touching any of the following require extra care:

- token signing or verification;
- token claims;
- expiration handling;
- token issuance;
- route, method, path, or query matching;
- replay protection;
- `ReplayStore` implementations;
- schema validation;
- request parsing;
- upstream credential injection;
- header forwarding;
- URL construction;
- upstream error handling;
- request or response size limits;
- admin authentication;
- secret handling.

For these changes, include tests that cover both the expected successful behavior and likely bypass attempts.

Examples of useful negative tests include:

- modified token signature is rejected;
- expired token is rejected;
- wrong route is rejected;
- wrong HTTP method is rejected;
- sibling or prefix-confusion path is rejected;
- path traversal attempts are rejected;
- unexpected query strings are rejected;
- unknown JSON fields are rejected;
- invalid nested object fields are rejected;
- invalid enum/range/length values are rejected;
- malformed JSON is rejected;
- oversized bodies are rejected;
- a consumed token cannot be replayed;
- concurrent replay attempts result in at most one successful consume;
- replay-store failure fails closed;
- caller-supplied `Authorization` does not become upstream authorization;
- forbidden injected headers are rejected at configuration time;
- sensitive upstream error bodies are not exposed.

If a change intentionally alters one of the invariants documented in [SECURITY.md](SECURITY.md), update the security documentation in the same pull request.

## ReplayStore contract

`ReplayStore` is part of the security boundary.

An implementation of:

```go
type ReplayStore interface {
    Consume(ctx context.Context, id string, expiresAt time.Time) (bool, error)
}
```

must behave atomically.

`Consume` should return:

- `true, nil` when the token ID was not previously consumed and has now been recorded until expiration;
- `false, nil` when the token ID has already been consumed;
- a non-nil error when the replay backend cannot reliably determine or record consumption.

It must not implement `Get` followed by `Set` as separate non-atomic operations.

For distributed stores, the consume-if-absent operation must be atomic across all gate instances. Expiration should not occur before the capability itself expires.

Backend failure must be surfaced as an error so the gate can fail closed.

## Schema changes

The built-in schema implementation is deliberately smaller than full JSON Schema.

When adding a constraint:

1. keep the behavior deterministic and easy to review;
2. reject ambiguous or unsupported forms rather than silently accepting them;
3. preserve closed-object behavior unless a deliberate security-model change is being proposed;
4. add success and failure tests;
5. include nested-object or array cases when relevant;
6. document newly supported syntax in the README.

A proposal to adopt a third-party JSON Schema implementation should discuss dependency risk, behavioral compatibility, object-closure semantics, and how unsupported or remote schema features are handled.

## Capability/token changes

Token format changes should be treated as protocol changes.

A proposal should explain:

- the threat being addressed;
- new signed claims;
- how old and new tokens interact;
- whether the change affects rolling deployments;
- key-rotation implications;
- replay implications;
- parsing and canonicalization rules;
- downgrade or confusion risks.

For argument-bound capabilities, canonicalization must be specified unambiguously before a request digest becomes part of the signed security boundary.

## Upstream URL and credential changes

Changes to upstream routing must be reviewed for SSRF, path-confusion, query-confusion, and credential-forwarding risks.

The caller should not be able to use capability-controlled input to select an arbitrary host or cause server-side credentials to be sent to an unintended destination.

New authentication mechanisms should keep real upstream credentials server-side and out of capability payloads and responses.

## API and compatibility expectations

The project is pre-1.0, so interfaces may evolve. Even so, avoid unnecessary breaking changes.

If a change affects:

- environment variables;
- route configuration JSON;
- public Go types or interfaces;
- token issuance responses;
- proxy behavior;
- status codes or error codes;

call it out explicitly in the pull request description and update documentation/examples as needed.

## Tests

New features and bug fixes should include tests whenever practical.

Prefer deterministic tests using `httptest` and injected clocks rather than sleeps or external services.

Security regressions should receive a test that fails before the fix and passes afterward.

Where concurrency matters, test concurrency directly and run with the race detector.

## Documentation

Documentation is part of the security surface.

Avoid claims stronger than the implementation actually provides. In particular, the current version does **not** bind capabilities to exact request argument values; it binds route, method, path, expiry, and token identity while separately enforcing a request schema.

Update README/security documentation whenever a change affects the threat model, assumptions, supported schema constraints, deployment model, or limitations.

## Pull request checklist

Before opening a PR, confirm that:

- [ ] the change is focused and explained clearly;
- [ ] `gofmt` has been run;
- [ ] `go test -race ./...` passes;
- [ ] `go vet ./...` passes;
- [ ] relevant negative/bypass tests were added for security-sensitive behavior;
- [ ] documentation was updated if behavior, configuration, or the threat model changed;
- [ ] no real credentials, tokens, private endpoints, or personal data were committed;
- [ ] new dependencies are justified;
- [ ] compatibility or rollout impact is described when applicable.

## Pull request description

A useful PR description answers:

### What changed?

Describe the implementation change concisely.

### Why?

Describe the problem, use case, or threat being addressed.

### Security impact

State whether the change affects authorization, credentials, token semantics, replay behavior, parsing, schema validation, networking, or information disclosure.

If there is no meaningful security impact, say so.

### How was it tested?

List the relevant automated tests and any manual verification.

### Compatibility

Note configuration, API, deployment, or rollout considerations.

## Issues

For bugs and feature requests, include enough context to reproduce or understand the problem:

- what you expected;
- what happened instead;
- minimal configuration or code needed to reproduce;
- Go version and operating environment;
- relevant logs with secrets removed.

Do **not** open a public issue for an exploitable vulnerability. Follow [SECURITY.md](SECURITY.md) instead.

## Secrets and test data

Never commit:

- real API keys;
- real bearer tokens;
- `GATE_ADMIN_KEY` values used outside local tests;
- production signing keys;
- real customer/user data;
- private infrastructure hostnames when they are sensitive;
- credentials embedded in examples or fixtures.

Examples should use obviously fake/demo credentials.

If a secret is accidentally committed, treat it as compromised and rotate it. Removing it from a later commit does not make the original secret safe.

## Dependency additions

`mcp-gate` currently keeps the core implementation small. New dependencies should be evaluated for:

- necessity;
- maintenance health;
- transitive dependency count;
- vulnerability history;
- license compatibility;
- whether equivalent behavior is practical with the Go standard library.

Dependency additions that touch cryptography, authentication, URL handling, schema validation, or serialization deserve particularly careful review.

## Performance changes

Performance work is welcome, but correctness and security take priority over micro-optimizations.

For meaningful performance claims, provide a reproducible benchmark and explain the workload being measured.

Do not weaken validation, replay protection, body limits, or error sanitization for performance without explicit design discussion.

## Commit messages

Clear commit messages are appreciated. A simple style such as the following works well:

```text
feat: add redis replay store
fix: reject scoped path confusion
security: harden upstream header filtering
test: cover concurrent token replay
docs: clarify argument-binding limitation
```

This convention is encouraged but not required.

## Review philosophy

For security-sensitive code, maintainers may ask for a smaller implementation, additional denial-path tests, clearer invariants, or more documentation even when the feature itself works.

The goal is not only to make the code function. The goal is to make the authorization path understandable enough to audit.

Thanks for helping improve `mcp-gate`.
