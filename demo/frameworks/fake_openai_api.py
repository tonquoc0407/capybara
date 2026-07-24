"""A local OpenAI-compatible endpoint, so real clients and real instrumentors
can be exercised without an API key.

The model's replies are scripted; everything around them — the client library,
the framework, the instrumentor, the transport — is the real thing. The point is
to see what those libraries actually put on a span, not to test a model.
"""

import json
import re
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

PORT = 8099
PRICE_CALL = {
    "id": "call_price_1",
    "type": "function",
    "function": {"name": "get_stock_price", "arguments": '{"symbol": "NVDA"}'},
}
# The turn that answers past a failed lookup, with a figure the tool never
# returned. This is the shape capybara's improvise check is looking for.
IMPROVISED = (
    "NVDA closed at $118.42 according to the get_stock_price lookup, "
    "so 120 shares are worth $14,210."
)


def wants_tool(messages):
    return not any(m.get("role") == "tool" for m in messages)


def tool_failed(messages):
    for m in messages:
        if m.get("role") == "tool":
            body = str(m.get("content", ""))
            if re.search(r"error|fail|502|timeout", body, re.I):
                return True
    return False


def reply(messages):
    if wants_tool(messages):
        return {"role": "assistant", "content": None, "tool_calls": [PRICE_CALL]}
    if tool_failed(messages):
        return {"role": "assistant", "content": IMPROVISED}
    return {
        "role": "assistant",
        "content": "The lookup returned a price; reporting it as given.",
    }


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *_):
        pass

    def do_POST(self):
        body = json.loads(self.rfile.read(int(self.headers["Content-Length"])))
        message = reply(body.get("messages", []))
        payload = {
            "id": "chatcmpl-fake",
            "object": "chat.completion",
            "created": int(time.time()),
            "model": body.get("model", "gpt-4o-mini"),
            "choices": [
                {
                    "index": 0,
                    "message": message,
                    "finish_reason": "tool_calls"
                    if message.get("tool_calls")
                    else "stop",
                }
            ],
            "usage": {
                "prompt_tokens": 1180,
                "completion_tokens": 62,
                "total_tokens": 1242,
            },
        }
        raw = json.dumps(payload).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(raw)))
        self.end_headers()
        self.wfile.write(raw)


if __name__ == "__main__":
    print(f"fake openai api on 127.0.0.1:{PORT}", flush=True)
    ThreadingHTTPServer(("127.0.0.1", PORT), Handler).serve_forever()
