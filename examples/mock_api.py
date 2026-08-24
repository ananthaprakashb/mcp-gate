"""Tiny upstream ticket API used only by the Docker Compose demo."""

import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from urllib.parse import parse_qs, urlparse


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):  # noqa: N802 - method name is defined by BaseHTTPRequestHandler
        if self.path == "/healthz":
            self.respond(200, {"status": "ok"})
        else:
            self.respond(404, {"error": "not found"})

    def do_POST(self):  # noqa: N802 - method name is defined by BaseHTTPRequestHandler
        parsed = urlparse(self.path)
        if parsed.path != "/v1/tickets":
            self.respond(404, {"error": "not found"})
            return
        if (
            self.headers.get("X-API-Key") != "demo-upstream-key"
            or parse_qs(parsed.query).get("tenant") != ["demo"]
        ):
            self.respond(401, {"error": "missing upstream credentials"})
            return

        length = int(self.headers.get("Content-Length", "0"))
        request = json.loads(self.rfile.read(length))
        self.respond(
            201,
            {
                "id": "ticket-demo-001",
                "title": request["title"],
                "priority": request.get("priority", "low"),
                "tenant": "demo",
            },
        )

    def log_message(self, format, *args):
        print(f"mock-api: {format % args}", flush=True)

    def respond(self, status, body):
        encoded = json.dumps(body).encode()
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)


if __name__ == "__main__":
    print("mock-api listening on :8081", flush=True)
    ThreadingHTTPServer(("0.0.0.0", 8081), Handler).serve_forever()
