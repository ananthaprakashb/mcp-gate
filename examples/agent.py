"""Minimal agent flow: mint a capability, then use it exactly once."""

import json
import os
import time
import urllib.error
import urllib.request


GATE_URL = os.environ.get("GATE_URL", "http://localhost:8080")
ADMIN_KEY = os.environ.get("GATE_ADMIN_KEY", "demo-admin-key")


def post(path, body, headers=None):
    request = urllib.request.Request(
        GATE_URL + path,
        data=json.dumps(body).encode(),
        headers={"Content-Type": "application/json", **(headers or {})},
        method="POST",
    )
    with urllib.request.urlopen(request, timeout=5) as response:
        return response.status, json.load(response)


def wait_for_gate():
    for _ in range(30):
        try:
            with urllib.request.urlopen(GATE_URL + "/readyz", timeout=1):
                return
        except (OSError, urllib.error.URLError):
            time.sleep(0.5)
    raise RuntimeError("mcp-gate did not become ready")


if __name__ == "__main__":
    wait_for_gate()
    _, capability = post(
        "/v1/tokens",
        {
            "route": "tickets",
            "method": "POST",
            "path": "/v1/tickets",
            "ttl_seconds": 15,
        },
        {"X-Gate-Key": ADMIN_KEY},
    )
    status, ticket = post(
        "/proxy/tickets/v1/tickets",
        {"title": "Investigate demo alert", "priority": "high"},
        {"Authorization": "Bearer " + capability["access_token"]},
    )
    print(f"Created through mcp-gate (HTTP {status}):")
    print(json.dumps(ticket, indent=2))
