# Local HTTP events

Start capybara's TUI, then press `i` and choose **HTTP JSON events**. For a service without a terminal UI, run `capybara listen`; it prints the endpoint and keeps the analyzer running. The default endpoint is `http://127.0.0.1:4318/api/v1/events`; the TUI and `listen` show the bound address if you moved it with `-otlp` or `CAPYBARA_OTLP_ADDR`.

This [GIF](../demo/pupu-tour.gif) shows a labelled synthetic failure sent through the local HTTP listener, then inspected in Connect, the span tree, and the cost view. Rebuild it with `vhs demo/local-api.tape` from the repository root. The tape uses a temporary database and disables Claude session watching, so it does not read local Claude Code sessions.

Send one JSON object per `POST`, with `Content-Type: application/json`. Every event needs `event_id`, `type`, `run_id`, `span_id` and an RFC3339 `time`. IDs are strings of at most 256 characters; span IDs must be unique across runs in one database. Keep the same event ID and body when retrying a request.

| Type | Required fields | Meaning |
| --- | --- | --- |
| `span.start` | `kind`, `name` | Open a span at `time`. |
| `span.update` | — | Merge supplied fields into an existing span. |
| `span.end` | `status` | Close an existing span at `time`. |
| `span.complete` | `kind`, `name`, `status` | Store a finished span in one request. Optional `start_time` sets its start; otherwise it starts at `time`. |

`kind` is `agent`, `llm`, `tool`, `retrieval` or `other`. End status is `ok` or `error`. Optional fields include `parent_id` on start, `model`, `provider`, `tool_name`, nonnegative `tokens_in`, `tokens_out`, and `cost_usd`, `attrs` (JSON object), and `contents` (array of `{role, body, media_type}`). Updates preserve fields they omit. A child can name its parent through `parent_id`.

Send `cost_usd` only when your application has the provider's reported amount. Capybara records it as application-reported provider cost and keeps it when later token or model events arrive; an automatic estimate cannot overwrite it. If you omit it, capybara can estimate cost from supported model pricing.

[`examples/local_events.py`](../examples/local_events.py) and [`examples/local_events.mjs`](../examples/local_events.mjs) send a start, update and end without an SDK. The Node example needs Node 18+. Both print the run ID and create a fresh run on each invocation. Each request retries up to twice after `503` or a connection failure, using the original event ID and body.

Responses: `201` stored; `200` exact retry; `400` invalid event; `403` nonlocal client; `409` update/end before start or conflicting event ID; `413` body over 1 MiB; `503` ingest busy with `Retry-After: 1`. The HTTP listener shares OTLP's bounded ingest capacity. The API accepts same-machine clients only and has no API key. Use `-no-content` to keep metadata while dropping content bodies and untrusted attributes.
