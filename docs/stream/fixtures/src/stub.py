#!/usr/bin/env python3
"""A stand-in for LaunchPad's missioncontrol, used only to record fixtures.

It answers GET /healthz and GET /api/launches on 127.0.0.1:8080 so that
examples/launchpad/*.chaos.yaml can run without LaunchPad installed.
"""
import json
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == "/healthz":
            body = {"ok": True}
        elif self.path == "/api/launches":
            body = [{"id": 1, "name": "ignite"}, {"id": 2, "name": "liftoff"}]
        else:
            self.send_response(404)
            self.end_headers()
            return
        data = json.dumps(body).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(data)))
        self.end_headers()
        self.wfile.write(data)

    def log_message(self, *args):
        pass


if __name__ == "__main__":
    ThreadingHTTPServer(("127.0.0.1", 8080), Handler).serve_forever()
