# mcp-gate

**Give an LLM permission to perform one narrowly defined tool action once — without giving it the real upstream credential.**

`mcp-gate` is a small Go reverse proxy for LLM/MCP-style tool execution. A trusted orchestrator exchanges its long-lived gate credential for a **short-lived, single-use, HMAC-signed capability token** bound to one configured route, HTTP method, and exact path.

Before forwarding a request, the gate validates the model-generated JSON against a closed schema, consumes the token atomically to prevent replay, and injects the real upstream credential server-side.

The project is intentionally small and uses Go's standard library for the core proxy and cryptographic path.

> **Security:** Please read [SECURITY.md](SECURITY.md) for the threat model, supported security assumptions, deployment guidance, and responsible disclosure process.
>
> **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md) for development setup, testing expectations, security-sensitive contribution guidance, and pull request requirements.

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

Issue a capability for one execution step:

```sh
curl -sS http://localhost:8080/v1/tokens \
  -H "X-Gate-Key: $GATE_ADMIN_KEY" -H 'Content-Type: application/json' \
  -d '{"route":"tickets","method":"POST","path":"/v1/tickets","ttl_seconds":15}'
```

Then send the returned token only to the matching proxy URL:

```sh
curl -sS http://localhost:8080/proxy/tickets/v1/tickets \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"title":"Investigate alert","priority":"high"}'
```

## Security model

- Policies cap TTL at 300 seconds and constrain route, method, and path.
- Tokens are consumed only after body validation and cannot be replayed.
- Object schemas are closed by design; unknown properties are rejected at every object level. Supported constraints are `type`, `properties`, `required`, `items`, `enum`, `pattern`, string lengths, numeric bounds, and array lengths.
- Upstream credentials never appear in capability tokens or responses. Routes can inject a bearer token, Basic Auth, fixed headers, and fixed query values.
- Non-2xx upstream bodies are replaced with a stable JSON error so stack traces, internal hostnames, and sensitive response data are not leaked.
- Request and response bodies are bounded (1 MiB by default); hop-by-hop and caller-supplied authentication headers are not forwarded.

### Current limitation: capabilities are not argument-bound

A capability is currently bound to route, method, exact path, TTL, and a unique token ID. The JSON body must match the configured closed schema, but the token does not currently commit to the exact argument values selected by the orchestrator.

That means the gate can enforce:

> `POST /v1/tickets` once, with only the fields and value ranges allowed by policy.

It cannot yet enforce:

> `POST /v1/tickets` once with exactly this canonical JSON body.

Argument-bound capabilities are a planned extension. One approach is to include a digest of a canonical request representation in the signed claims and compare it before token consumption.

## What mcp-gate is — and is not

`mcp-gate` is a narrow authorization boundary for LLM/tool execution. It is intended to reduce the authority exposed to an agent and keep upstream credentials outside model context.

It is **not**:

- an identity provider;
- a replacement for OAuth, workload identity, mTLS, or service-to-service authentication;
- a complete policy engine;
- a full JSON Schema implementation;
- protection against compromise of the trusted orchestrator, gate host, signing key, or upstream service;
- a substitute for TLS, ingress authentication, rate limiting, network controls, observability, or secret management.

See [SECURITY.md](SECURITY.md) for the detailed threat model and production deployment expectations.

## Distributed replay protection

The default replay store is process-local. Applications embedding `gate` can provide `Config.ReplayStore`, whose atomic `Consume` operation is suitable for a Redis/Valkey `SET key value NX EX ttl` adapter. Store errors fail closed with HTTP 503, so a backend outage cannot silently disable single-use enforcement.

For more than one `mcp-gate` process, a shared replay store is required if single-use behavior must hold across instances.

## Operational endpoints

`GET /healthz` and `GET /readyz` provide liveness and readiness probes. Logs from the executable use Go's structured `slog` format.

## Container

Build and run the non-root distroless image:

```sh
docker build -t mcp-gate .
docker run --rm -p 8080:8080 \
  -e GATE_SIGNING_KEY -e GATE_ADMIN_KEY -e GATE_ROUTES mcp-gate
```

TLS termination and rate limiting should be supplied by the surrounding ingress.

## CI and verification

The GitHub Actions workflow runs:

```text
go test -race ./...
go vet ./...
govulncheck ./...
```

Security-sensitive changes should include tests that exercise both the allowed path and the denied/bypass path. See [CONTRIBUTING.md](CONTRIBUTING.md).

## Roadmap

Potential next steps include:

- argument-bound capabilities;
- Redis/Valkey replay-store adapters;
- asymmetric signing and key rotation;
- richer policy constraints;
- rate and execution-budget controls;
- security/audit events;
- MCP-native integration examples;
- OpenTelemetry support;
- Kubernetes deployment examples.

## Contributing

Security reviews, threat-model critiques, implementation feedback, bug reports, and focused pull requests are welcome.

Before contributing, please read [CONTRIBUTING.md](CONTRIBUTING.md). If you believe you have found a vulnerability, **do not open a public issue**; follow the private reporting guidance in [SECURITY.md](SECURITY.md).

## License

MIT
