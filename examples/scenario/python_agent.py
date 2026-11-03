"""Example subprocess adapter; replace decide() with a call to your agent.

The harness sends one JSON request on stdin and expects one JSON response on
stdout. This process runs with normal host permissions; mock tools are only
supplied through the protocol, not enforced against arbitrary user code.
"""
import json
import sys


def decide(request):
    messages = request["messages"]
    last = messages[-1]
    if last["role"] == "tool" and last["name"] == "weather":
        value = last["json"]
        return {"assistant": "Weather: " + value["forecast"], "cost_usd": 0}
    if "weather" in last.get("content", "").lower():
        return {"tool_calls": [{"name": "weather", "arguments": {}}], "cost_usd": 0}
    return {"assistant": "Acknowledged: " + last.get("content", ""), "cost_usd": 0}


print(json.dumps(decide(json.load(sys.stdin))))
