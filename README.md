# mcp-gate

`mcp-gate` is a small reverse proxy for LLM tool execution. An orchestrator
exchanges its long-lived gate credential for a **single-use, HMAC-signed bearer
token** bound to one configured route, HTTP method, exact path, and short TTL.
The proxy validates the JSON body against a closed schema before forwarding it,
so model-generated arguments cannot add undeclared fields.

## Run

Requires Go 1.22+. Route policies, upstream credentials, and signing material
stay server-side:

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
- Object schemas are closed by design; unknown properties are rejected at every
  object level. Supported constraints are `type`, `properties`, `required`,
  `items`, `enum`, `pattern`, string lengths, numeric bounds, and array lengths.
- Upstream credentials never appear in capability tokens or responses. Routes
  can inject a bearer token, Basic Auth, fixed headers, and fixed query values.
- Non-2xx upstream bodies are replaced with a stable JSON error so stack traces,
  internal hostnames, and sensitive response data are not leaked.
- Request and response bodies are bounded (1 MiB by default); hop-by-hop and
  caller-supplied authentication headers are not forwarded.

`GET /healthz` and `GET /readyz` provide liveness and readiness probes. Logs from
the executable use Go's structured `slog` format.

## Distributed replay protection

The default replay store is process-local. Applications embedding `gate` can
provide `Config.ReplayStore`, whose atomic `Consume` operation is suitable for
a Redis/Valkey `SET key value NX EX ttl` adapter. Store errors fail closed with
HTTP 503, so a backend outage cannot silently disable single-use enforcement.

## Container

Build and run the non-root distroless image:

```sh
docker build -t mcp-gate .
docker run --rm -p 8080:8080 \
  -e GATE_SIGNING_KEY -e GATE_ADMIN_KEY -e GATE_ROUTES mcp-gate
```

TLS termination and rate limiting should be supplied by the surrounding
ingress.
