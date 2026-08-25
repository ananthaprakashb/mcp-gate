# Security Policy

`mcp-gate` is security-sensitive infrastructure. Its purpose is to reduce the authority exposed to an LLM or tool executor by exchanging broad, long-lived application credentials for narrowly scoped, short-lived, single-use capabilities.

This document describes the current threat model, security guarantees, non-goals, production deployment expectations, and vulnerability reporting process.

## Project status and supported versions

`mcp-gate` is an early-stage project and the security model may evolve as the API matures.

Until tagged releases are established, security fixes are applied to the `main` branch. Once versioned releases are published, this section will identify the supported release lines.

Users deploying the project should track security-related changes and keep deployments current.

## Security objective

The core goal is to make an authorization decision more precise than:

> This agent has a credential that can call the upstream service.

The intended model is closer to:

> This holder may perform this HTTP method against this exact configured route and path, once, before a short expiration time, with a JSON body that satisfies the server-side policy.

The upstream credential remains on the gate and is injected only after the capability and request have passed validation.

## Trust boundaries

The current design assumes the following components are trusted:

- the orchestrator that holds `GATE_ADMIN_KEY` and requests capabilities;
- the `mcp-gate` process and host/container runtime;
- the signing key and route configuration supplied to the gate;
- the configured replay store, when one is provided;
- the upstream API and its credential-management system;
- the TLS/ingress layer protecting traffic to and from the gate.

The LLM, model-generated arguments, agent runtime, and capability holder should be treated as less trusted.

## What the current implementation protects against

### Broad credential exposure

The capability token does not contain the upstream API credential. The gate can inject bearer credentials, Basic Auth, fixed headers, and fixed query values after validating the request.

This keeps reusable upstream credentials out of model prompts, model context, and normal capability responses.

### Capability scope expansion

A token is signed and bound to:

- a configured route;
- an HTTP method;
- an exact path;
- an expiration timestamp;
- a unique token identifier used for replay protection.

Requests whose route, method, path, or expiry do not match are rejected.

### Replay of a successfully authorized action

Capabilities are intended to be single-use.

The token identifier is consumed atomically only after the request body passes validation. A second request using the same successfully consumed capability is rejected.

The default replay store is process-local. If multiple gate instances are used, applications must provide a shared `ReplayStore` implementation to preserve single-use semantics across instances.

A distributed implementation must provide an atomic consume-if-absent operation with an expiration at least as long as the token's remaining lifetime. Redis/Valkey `SET key value NX EX ttl`-style semantics are an appropriate model.

Replay-store errors fail closed with HTTP 503; a replay-store outage must not silently disable single-use enforcement.

### Model-generated undeclared JSON fields

Object schemas are closed by design. If a model adds a property that is not declared in the configured schema, the request is rejected rather than forwarded.

The schema implementation is intentionally limited and auditable. Supported constraints currently include:

- `type`;
- `properties`;
- `required`;
- `items`;
- `enum`;
- `pattern`;
- string length bounds;
- numeric bounds;
- array length bounds.

Validation occurs before capability consumption so a malformed request does not burn an otherwise valid one-time token.

### Credential/header smuggling to the upstream service

Caller-supplied authentication is not forwarded as upstream authentication. Upstream credentials are constructed from server-side route policy.

Sensitive or hop-by-hop headers such as `Authorization`, `Connection`, `Content-Length`, `Host`, and `Transfer-Encoding` cannot be configured as arbitrary injected headers.

### Leakage through upstream error bodies

Non-2xx upstream response bodies are not returned to the caller. The gate replaces them with stable JSON errors to reduce the chance of exposing stack traces, internal hostnames, credentials, service topology, or other sensitive backend information.

### Oversized request/response bodies

Request and response bodies are bounded. The default maximum body size is 1 MiB unless configured otherwise.

## Important current limitation: capabilities are not argument-bound

The signed capability currently constrains the route, method, exact path, TTL, and unique token ID. The request body must satisfy the configured schema, but the token does **not** currently commit to exact argument values selected by the orchestrator.

For example, a policy may allow:

```json
{
  "title": "string",
  "priority": "low | high"
}
```

A capability for that route can be used with any values that satisfy the allowed schema until the token is consumed.

It cannot yet express:

> Execute exactly this canonical JSON request body once.

Argument-bound capabilities are a planned improvement. A likely design is to include a digest of a canonical request representation in the signed claims and verify the digest before replay consumption and forwarding.

Do not describe the current implementation as providing exact argument-value binding.

## Non-goals and attacks outside the current security boundary

`mcp-gate` does not currently protect against every compromise in an agent system.

### Compromised trusted orchestrator

An attacker controlling the trusted orchestrator or `GATE_ADMIN_KEY` can request capabilities permitted by configured route policies. Protect the admin credential as a high-value service credential and keep it out of model context.

### Compromised gate host or signing key

An attacker with control of the `mcp-gate` runtime, process memory, signing key, or route configuration is inside the trusted computing base.

Use normal production controls for host/container isolation, secret management, patching, and least privilege.

### Compromised upstream service

The gate cannot make a malicious or compromised upstream service trustworthy.

### Prompt injection by itself

`mcp-gate` is not a prompt-injection detector. It reduces the authority available after a tool action has been authorized. The orchestrator remains responsible for deciding whether a requested capability should be issued.

### Identity and authentication

`mcp-gate` is not an identity provider and does not replace OAuth, workload identity, mTLS, service-to-service authentication, or user authorization.

It is intended to complement those systems by narrowing execution authority at the tool boundary.

### Network security

The server does not provide production TLS termination or network-layer rate limiting. Deploy it behind an appropriate ingress, load balancer, service mesh, or reverse proxy.

### Full policy-language enforcement

The built-in schema validator is intentionally not a full JSON Schema implementation, and `mcp-gate` is not a general-purpose authorization policy engine.

## Production deployment guidance

For production or security-sensitive environments, at minimum:

1. **Terminate TLS at a trusted ingress or service mesh.** Do not expose capability or admin credentials over plaintext networks.
2. **Protect `GATE_ADMIN_KEY`.** Only trusted orchestration components should be able to issue capabilities.
3. **Use a strong signing key.** `mcp-gate` requires at least 32 characters; use randomly generated high-entropy secret material and a proper secret manager.
4. **Do not put upstream secrets in client-visible configuration, prompts, logs, or capability metadata.**
5. **Use the shortest practical TTL.** Route policies already cap TTL at 300 seconds; most tool executions should need substantially less.
6. **Use a shared replay store when running more than one instance.** Process-local replay protection cannot prevent reuse against a different instance.
7. **Make replay-store failure fail closed.** Custom `ReplayStore` implementations must preserve the atomic single-use contract.
8. **Keep schemas narrow.** Declare only fields and ranges the tool genuinely requires. Closed schemas are most useful when policies are least-privilege.
9. **Restrict upstream network access.** Ideally, the upstream API should accept traffic only from the gate or its trusted network boundary.
10. **Rate-limit token issuance and proxy traffic at the surrounding infrastructure.**
11. **Restrict access to operational endpoints as appropriate for your environment.**
12. **Review logs before centralizing them.** Never intentionally log capability tokens, signing keys, gate admin keys, upstream credentials, or sensitive request bodies.
13. **Keep dependencies and the Go toolchain current.** CI runs tests, `go vet`, and `govulncheck`, but deployment owners remain responsible for patching deployed artifacts.
14. **Treat route policy changes like authorization-code changes.** Review them carefully and test denial cases.

## Secret rotation

The current token format uses a single HMAC signing key. There is not yet built-in multi-key verification or key-ID based rotation.

Rotating `GATE_SIGNING_KEY` immediately invalidates capabilities signed with the previous key. Because capabilities are deliberately short-lived, this may be acceptable for many deployments, but operators should account for this behavior during rotation.

Asymmetric signing and explicit key-rotation support are roadmap items.

`GATE_ADMIN_KEY` and upstream credentials should be rotated using the surrounding secret-management system. Avoid overlapping old and new credentials longer than necessary.

## Security-sensitive implementation invariants

Changes to the project should preserve these invariants unless the security model is deliberately revised and documented:

- capability signatures are verified before claims are trusted;
- expired capabilities are rejected;
- route, method, and exact path are checked against signed claims;
- query strings supplied by capability callers are not accepted as a way to extend signed scope;
- request JSON is parsed and validated before replay consumption;
- replay consumption is atomic;
- replay-store failure fails closed;
- upstream credentials are injected server-side only after authorization succeeds;
- caller authentication headers are not blindly forwarded upstream;
- non-2xx upstream response bodies are not exposed to the caller;
- request and response bodies remain bounded;
- unknown object properties remain rejected unless a future policy model explicitly changes that behavior.

Pull requests affecting these properties should include negative/bypass tests in addition to successful-path tests.

## Reporting a vulnerability

Please **do not open a public GitHub issue containing vulnerability details, exploit code, secrets, or instructions that would enable active abuse**.

Preferred reporting path:

1. Open the repository's **Security** tab and use **Report a vulnerability** / private vulnerability reporting if the option is available.
2. Include enough information to reproduce and assess the issue without including unrelated secrets or personal data.
3. If private vulnerability reporting is not available, open a minimal public issue stating only that you need a private channel for a security report. Do not disclose the vulnerability details publicly. A maintainer can then arrange a private reporting path.

A useful report includes:

- affected commit, branch, or release;
- affected component or endpoint;
- threat scenario and required attacker capabilities;
- reproducible steps or a minimal proof of concept;
- expected behavior versus observed behavior;
- security impact;
- suggested mitigation, if known.

Please avoid testing against systems you do not own or have explicit permission to assess.

## Coordinated disclosure

For credible vulnerabilities, maintainers will aim to:

- acknowledge the report;
- reproduce and assess impact;
- prepare a fix and regression test;
- coordinate disclosure timing with the reporter when practical;
- credit the reporter if they want attribution;
- avoid publishing exploit-enabling detail before users have a reasonable opportunity to update.

Because this is an early-stage open-source project, fixed response-time guarantees are not currently offered.

## Security research and review are welcome

Threat-model critique, protocol review, fuzzing, bypass tests, replay-store analysis, schema-validation review, and deployment-hardening suggestions are encouraged.

For non-sensitive security design discussions, open a normal GitHub issue. For exploitable vulnerabilities, use the private reporting process above.
