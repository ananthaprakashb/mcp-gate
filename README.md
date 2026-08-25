# mcp-gate

**Give an LLM permission to perform one narrowly defined tool action once — without giving it the real upstream credential.**

`mcp-gate` is a small Go reverse proxy for LLM/MCP-style tool execution. A trusted orchestrator exchanges its long-lived gate credential for a **short-lived, single-use, HMAC-signed capability token** bound to one configured route, HTTP method, and exact path.

Before forwarding a request, the gate validates the model-generated JSON against a closed schema, consumes the token atomically to prevent replay, and injects the real upstream credential server-side.

The project is intentionally small and uses Go's standard library for the core proxy and cryptographic path.

## Why?

LLM agents often need to call APIs that are much more powerful than the individual action the model is trying to perform.

If an agent receives a reusable API key, OAuth token, or broadly privileged tool credential, the effective authority can become:

> Call anything this credential can access for as long as the credential remains valid.

`mcp-gate` is an experiment in making that authority much narrower:

> `POST /v1/tickets` once, within the next 15 seconds, with a body that matches this allowed schema.

The model never needs the upstream API secret.

## How it works

```text
LLM / Agent
    |
    | asks to perform a tool action
    v
Trusted orchestrator
    |
    | POST /v1/tokens
    | route + method + exact path + short TTL
    v
mcp-gate
    |
    | returns single-use capability token
    v
LLM / tool executor
    |
    | capability token + JSON arguments
    v
mcp-gate
    |
    | verify signature + expiry + scope
    | validate JSON body
    | atomically consume token
    | inject upstream credentials
    v
Upstream API
```

The capability token contains no upstream credential.

## Try the end-to-end demo

The fastest way to evaluate the project is the included Docker Compose example. It starts `mcp-gate`, a mock ticket API, and a dependency-free Python agent that obtains a capability and creates a ticket through the gate.

```sh
git clone https://github.com/ananthaprakashb/mcp-gate.git
cd mcp-gate/examples
docker compose up --build --abort-on-container-exit --exit-code-from agent
```

The agent prints the ticket created through `mcp-gate`, and Compose then stops the demo services.

The example uses demo credentials and is intended only for local evaluation.

## Run it directly

Requires Go 1.22+.

Route policies, upstream credentials, and signing material stay server-side:

```sh
export GATE_SIGNING_KEY='replace-with-at-least-32-random-characters'
export GATE_ADMIN_KEY='orchestrator-to-gate-secret'
export GATE_ROUTES='[
  {
    "name":"tickets",
    "upstream":"https://api.example.com",
    "path_prefix":"/v1/tickets",
    "methods":["POST"],
    "max_ttl_seconds":30,
    "upstream_auth":{
      "headers":{"X-API-Key":"external-api-secret"},
      "query":{"tenant":"agents"}
    },
    "request_schema":{
      "type":"object",
      "properties":{
        "title":{"type":"string","minLength":1,"maxLength":120},
        "priority":{"type":"string","enum":["low","high"]}
      },
      "required":["title"]
    }
  }
]'

go run ./cmd/mcp-gate
```

### 1. Issue a capability

The trusted orchestrator requests permission for one execution step:

```sh
curl -sS http://localhost:8080/v1/tokens \
  -H "X-Gate-Key: $GATE_ADMIN_KEY" \
  -H 'Content-Type: application/json' \
  -d '{"route":"tickets","method":"POST","path":"/v1/tickets","ttl_seconds":15}'
```

A successful response contains a bearer token scoped to that route, method, exact path, and expiration time.

### 2. Execute the tool call

Send that token only to the matching proxy URL:

```sh
curl -sS http://localhost:8080/proxy/tickets/v1/tickets \
  -H "Authorization: Bearer $TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Investigate alert","priority":"high"}'
```

After one valid execution, replaying the same token is rejected.

## What the gate enforces

| Control | Behavior |
| --- | --- |
| Route scope | Token is bound to one configured route |
| HTTP method | Token is valid only for the authorized method |
| Exact path | Proxy request must exactly match the path in the token |
| Short lifetime | Route policy limits TTL; the hard maximum is 300 seconds |
| Single use | A successfully validated request consumes the token |
| Request shape | JSON is validated against a closed schema before forwarding |
| Secret isolation | Upstream credentials stay in gate configuration, not capability tokens |
| Error isolation | Non-2xx upstream response bodies are replaced with stable JSON errors |
| Body limits | Request and response bodies are bounded; default is 1 MiB |

## Request validation

Tool arguments are part of the security boundary.

For example, if a tool is intended to accept only:

```json
{
  "title": "Investigate alert",
  "priority": "high"
}
```

an agent should not be able to add an undeclared field such as:

```json
{
  "title": "Investigate alert",
  "priority": "high",
  "admin": true
}
```

Object schemas in `mcp-gate` are closed by design. Unknown properties are rejected at every object level.

The deliberately small schema implementation currently supports:

- `type`
- `properties`
- `required`
- `items`
- `enum`
- `pattern`
- `minLength` / `maxLength`
- `minimum` / `maximum`
- `minItems` / `maxItems`

Schema validation occurs **before** token consumption. If the model produces malformed arguments, the caller can correct them without losing the capability. The token is consumed only when the request has passed validation and is ready to be forwarded.

## Upstream credentials stay server-side

Routes can inject credentials and fixed request context after caller-supplied authentication headers have been discarded.

Supported upstream authentication/configuration includes:

- bearer tokens
- Basic Auth
- fixed headers
- fixed query parameters

For example, an agent can call:

```text
POST /proxy/tickets/v1/tickets
Authorization: Bearer <ephemeral-capability>
```

while `mcp-gate` sends the request upstream with a server-side API key that the model never sees.

## Replay protection

The default replay store is process-local.

Applications embedding the `gate` package can provide `Config.ReplayStore`. Its atomic `Consume` operation is designed so a distributed implementation can use a Redis/Valkey operation such as `SET key value NX EX ttl`.

Replay-store failures fail closed with HTTP 503. A replay backend outage therefore cannot silently turn off single-use enforcement.

For a multi-replica production deployment, use a distributed replay store rather than the default in-memory implementation.

## Safe upstream failures

Upstream error bodies can contain stack traces, internal hostnames, implementation details, or secrets.

For non-2xx responses, `mcp-gate` drains the upstream body but does not return it to the agent. The caller receives a stable JSON error instead.

Caller-provided authentication headers are not forwarded upstream, and sensitive hop-by-hop headers are restricted.

## Health and operations

Liveness and readiness endpoints:

```text
GET /healthz
GET /readyz
```

The executable uses Go's structured `slog` logging.

TLS termination and rate limiting are intentionally expected to be provided by the surrounding ingress or service mesh.

## Container

Build and run the non-root distroless image:

```sh
docker build -t mcp-gate .
docker run --rm -p 8080:8080 \
  -e GATE_SIGNING_KEY \
  -e GATE_ADMIN_KEY \
  -e GATE_ROUTES \
  mcp-gate
```

## Threat model and non-goals

`mcp-gate` is intentionally narrow. It is an authorization boundary for tool execution, not a complete agent-security platform.

### What it is trying to reduce

- exposing long-lived upstream credentials to an LLM or tool executor
- allowing a capability issued for one endpoint to be reused for another
- replaying a valid capability multiple times
- model-generated JSON adding undeclared fields
- leaking sensitive upstream error bodies back into model context

### What it does not solve

- **Orchestrator compromise.** The holder of `GATE_ADMIN_KEY` is trusted. If it is compromised, an attacker can mint any capability allowed by configured route policies.
- **Exact argument authorization.** Today the capability is bound to route, method, path, and TTL. The request body must match the configured schema, but the token is not yet bound to the exact argument values approved by the orchestrator.
- **Identity.** This is not an identity provider and is not a replacement for OAuth, mTLS, workload identity, or user authentication.
- **Full JSON Schema.** The schema validator is deliberately a small, auditable subset rather than a complete JSON Schema implementation.
- **Distributed replay protection by default.** The bundled replay store is process-local. Multiple replicas require a shared `ReplayStore` implementation.
- **Ingress security.** TLS termination, network policy, DDoS protection, and rate limiting belong outside the gate.
- **The MCP protocol itself.** `mcp-gate` is currently an HTTP authorization proxy intended for MCP-style/LLM tool execution. It is not a full MCP server or MCP transport implementation.

## Why not just OAuth?

OAuth answers an important but different question: **who or what has been granted access?**

`mcp-gate` focuses on reducing authority at the moment an agent executes a tool:

```text
POST this exact path once within a few seconds.
```

It can sit behind an existing identity/authentication system rather than replacing one. An orchestrator can authenticate using whatever mechanism the surrounding platform already trusts, then mint a much narrower execution capability for the agent-facing step.

## Why single-use instead of only short-lived?

A 30-second token for a create operation can otherwise mean "create as many objects as possible for 30 seconds."

For many agent steps the intended authority is closer to:

> Perform this operation once.

That is why replay prevention is a first-class part of the design rather than relying on expiration alone.

## Why HMAC?

The current implementation uses HMAC-SHA256 because token issuance and verification happen within the gate trust boundary and the implementation remains compact and auditable.

Asymmetric signing and key rotation are reasonable future directions if deployments need separate issuers/verifiers or more sophisticated key distribution.

## Current roadmap

Areas worth exploring next include:

- **argument-bound capabilities** — bind a token to a canonical request-body hash so the orchestrator can authorize an exact operation, not only a schema-valid one
- Redis/Valkey replay-store adapters
- asymmetric signing and key rotation
- richer policy constraints
- per-capability rate, cost, or execution budgets
- structured audit events
- OpenTelemetry instrumentation
- MCP-native integration examples
- Kubernetes deployment examples

The project is intentionally being kept small before expanding the policy surface.

## Security-oriented CI

CI runs:

```text
go test -race ./...
go vet ./...
govulncheck ./...
```

## Feedback

The design question behind `mcp-gate` is simple:

> Instead of asking whether an agent is broadly authenticated, can we give it only the authority required for the next tool execution — and make that authority expire after one use?

Feedback, threat-model critiques, integration examples, and pull requests are welcome.

## License

MIT. See [`LICENSE`](LICENSE).
