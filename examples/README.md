# End-to-end Docker Compose demo

This demo starts `mcp-gate`, a mock ticket API, and a small Python agent. The
agent requests a route-bound, single-use capability and uses it to create one
ticket through the gate. The mock API also verifies the header and query
credential injected by the gate.

From this directory, run:

```sh
docker compose up --build --abort-on-container-exit --exit-code-from agent
```

The `agent` output ends with the ticket returned by the mock API. Compose stops
the demo after the agent exits. Remove the containers and network afterward:

```sh
docker compose down
```

All credentials in this example are intentionally public demo values. Never
reuse them in a real deployment.
