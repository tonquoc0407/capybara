"""Send a live span to a running local capybara TUI with Python's standard library."""

import json
import os
import time
import urllib.error
import urllib.request
import uuid
from datetime import datetime, timezone


endpoint = os.getenv("CAPYBARA_EVENTS_URL", "http://127.0.0.1:4318/api/v1/events")
run_id = uuid.uuid4().hex
span_id = uuid.uuid4().hex


def send(event_type, **fields):
    event = {
        "event_id": uuid.uuid4().hex,
        "type": event_type,
        "run_id": run_id,
        "span_id": span_id,
        "time": datetime.now(timezone.utc).isoformat(),
        **fields,
    }
    request = urllib.request.Request(
        endpoint,
        data=json.dumps(event).encode(),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    for attempt in range(3):
        try:
            with urllib.request.urlopen(request, timeout=5) as response:
                return json.load(response)
        except urllib.error.HTTPError as exc:
            if exc.code != 503 or attempt == 2:
                raise
            delay = min(float(exc.headers.get("Retry-After", "1")), 2)
            exc.close()
        except (urllib.error.URLError, TimeoutError):
            if attempt == 2:
                raise
            delay = 0.2 * 2**attempt
        time.sleep(max(0, delay))


if __name__ == "__main__":
    send("span.start", kind="llm", name="example model call", model="gpt-4o-mini")
    send("span.update", tokens_in=12)
    send("span.end", status="ok", tokens_out=8)
    print(run_id)
