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
    "upstream_bearer":"external-api-secret",
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
- Upstream bearer secrets never appear in capability tokens or responses.
- Request and response bodies are bounded (1 MiB by default); hop-by-hop and
  caller-supplied authentication headers are not forwarded.

Replay state is in memory, so deploy a single instance or add shared replay
storage before horizontally scaling. TLS termination and rate limiting should
be supplied by the surrounding ingress.
