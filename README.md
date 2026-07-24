<p align="center">
  <img src="demo/logo.png" alt="capybara logo" width="96">
</p>

<h1 align="center">capybara</h1>

<p align="center">
  A terminal trace debugger for AI agents.<br>
  It records what an agent did and shows you where it went wrong.
</p>

<p align="center">
  <a href="https://github.com/tonquoc0407/capybara/releases"><img src="https://img.shields.io/github/v/release/tonquoc0407/capybara?label=release" alt="release"></a>
  <a href="https://github.com/tonquoc0407/capybara/actions"><img src="https://img.shields.io/github/actions/workflow/status/tonquoc0407/capybara/ci.yml?branch=main" alt="build status"></a>
  <a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/tonquoc0407/capybara" alt="go version"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/tonquoc0407/capybara" alt="license"></a>
</p>

<p align="center">
  <img src="demo/demo.gif" alt="capybara demo" width="720">
</p>

## Contents

- [Install](#install)
- [Getting a trace in](#getting-a-trace-in)
- [Reading a trace](#reading-a-trace)
- [What it looks for](#what-it-looks-for)
- [Other commands](#other-commands)
- [Config](#config)
- [Architecture](#architecture)

## Install

```sh
go install github.com/tonquoc0407/capybara/cmd/capybara@latest
```

Or grab a binary from the [releases page](https://github.com/tonquoc0407/capybara/releases). One file, no CGo, no runtime dependencies.

## Getting a trace in

Run it with no arguments. It opens the TUI, listens for OTLP on `127.0.0.1:4318` and `127.0.0.1:4317`, and tails `~/.claude/projects` when that directory exists:

```sh
capybara
```

Traces land in `capybara.db` in the working directory. Point somewhere else with `-db`, and drop prompt and tool bodies with `-no-content`. With an empty database, the middle pane shows what it's listening on and how to send it something — there's nothing to look up.

If something else already holds 4317 or 4318 — a collector, Jaeger, another tracing UI — capybara keeps the transport that did bind, reports which one it lost, and carries on. `-otlp 127.0.0.1:4319` moves the HTTP listener somewhere free.

There are three ways in, and one database can hold all of them.

### OTLP

Point any instrumented app at it:

```sh
OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318
```

Four attribute conventions are read — OpenTelemetry's own `gen_ai.*`, OpenInference (Arize/Phoenix), OpenLLMetry (traceloop), and the Vercel AI SDK's legacy `ai.*` — so whichever instrumentor you already run is enough.

[`demo/frameworks/`](demo/frameworks) has two runnable agents that end in the same failure — a LangGraph one traced by OpenLLMetry and a plain OpenAI tool loop traced by OpenInference. Both point at a local stub of the OpenAI API, so they need no key.

Below, the LangGraph one runs while the TUI is open: the quote tool raises, LangGraph hands the failure back to the model, and the model answers with a price anyway.

<p align="center">
  <img src="demo/frameworks.gif" alt="a real LangGraph agent traced into capybara" width="720">
</p>

### A session file

```sh
capybara watch claude
```

Tails Claude Code's own logs. No instrumentation, no restart — it reads what's already on disk.

### A file you have

```sh
capybara import trace.jsonl
```

Takes span-per-line JSONL, or agent-replay JSON when the name ends in `.json`.

### From your own agent

Install the Python SDK:

```sh
pip install capybara-sdk
```

```python
import capybara

capybara.init()  # export to a local capybara on 127.0.0.1:4318


@capybara.trace(tool="lookup_price")
def lookup_price(sku: str) -> dict:
    return {"price": 42.0, "currency": "USD"}
```

`init()` reuses a `TracerProvider` you already configured, so any instrumentor you already run keeps working — capybara only adds an exporter to it.

## Reading a trace

The tree marks `x` for a failed span and `!` for one carrying a finding. The run column shows when each run started, what it cost, and what was recorded against it.

| Key | Action | Key | Action |
| --- | --- | --- | --- |
| `j` / `k` | move | `w` | cost waterfall |
| `enter` | expand | `d` | diff two runs |
| `/` | search | `c` | context view |
| `tab` | change pane | `b` | blame the output |
| `f` | filter by kind | `r` | re-run from a span |
| `a` | raw attributes | `t` | export a pytest case |
| `e` | edit a tool output | `?` | full help |
| `q` | quit | | |

The waterfall sorts spans by cost, so the turn that spent the money is the first line rather than something to scroll for. The context view shows what filled each turn's window — system text, tool output, history — and marks the turns where it dropped, which is where a compaction ate something.

## What it looks for

Findings are recorded, never enforced — capybara doesn't fail your build. Seven kinds, all passive.

### `improvised`

The one worth knowing about. A tool call failed, and the next model turn answered as if it hadn't:

```
tool get_stock_price x                    3.0s
  missing field: as_of (+3)
llm messages.create !                    17.0s
  improvised after get_stock_price failure
```

The evidence sits beside the answer, so the invented number is on screen next to the call that never returned it. A turn that retries the tool, or that says in any of several languages that it failed, isn't flagged.

### `drift`

Fires when a tool's output stops matching the shape it's been returning — a field disappears, or changes type. capybara learns that shape from what it observes, one call at a time, and adopts the new one at the change point. A tool that sometimes prints text and sometimes prints JSON has both accepted: one line of JSON out of a shell command isn't a contract change.

### `malformed` and `empty_payload`

Cover output that won't parse, or that arrived empty — and only for tools whose output has always been structured.

### `tool_error`

Marks a call that succeeded as far as the trace is concerned but whose payload says otherwise — an `error` field with something in it, MCP's `isError`, `ok: false`, an HTTP status in the 4xx or 5xx range, or text that opens with `Error:`, `fatal:` or a traceback. Frameworks differ here: a tool that raises is marked failed by the instrumentor and needs nothing extra, while a tool that returns an error value instead looks like a clean result.

Only the top-level keys or the first line are read — searching the whole body for the word "error" would flag every document that discusses one. On 1,964 real tool calls this fires 6 times, all of them genuine.

### `loop`

Marks the same call repeated back to back with the same arguments. A `Read` over ten files is a plan; the same `Read` ten times is a loop. Calls whose arguments were never recorded aren't compared against each other, because nothing is known about them.

### `cost_spike`

Marks a turn burning several times the run's own rolling baseline.

If your tool has a declared schema, give it to the SDK with `capybara.schema("lookup_price", Price)`. The declared shape wins over the learned one, so the first violating call is a finding instead of a new version.

## Other commands

| Command | Description |
| --- | --- |
| `capybara watch claude` | tail a session source without the TUI |
| `capybara diff <run_a> <run_b>` | align spans, mark the first divergence |
| `capybara blame <run>` | walk the final output back to its tainted source |
| `capybara replay <run>` | re-run a recording, optionally with an edited tool result |
| `capybara export <run>` | write a pytest case for the failure |
| `capybara export <run> --golden` | write a CI fixture |
| `capybara export <run> --html` | write a self-contained page |
| `capybara check <golden> <run>` | compare against a golden, non-zero on divergence |
| `capybara serve` | read-only web view |

`replay` serves the recorded model responses and tool outputs back to the agent process, so nothing touches the network. Edit one tool result first and only the turns after it go live — that's how you ask what the agent would have done with the answer it should have got. A call that isn't in the recording stops the replay rather than running live.

`serve` and `export --html` render the same read-only page, one from the database and one with a single run inlined. Recorded bodies are written as text, never as markup.

## Config

`~/.config/capybara/config.toml`:

```toml
theme = "bara"
```

`bara` is the default warm dark. `mono` drops the accent to grey; `paper` is for a light terminal. Red and amber mean the same thing in all three.

Model rates live in a table built into the binary, covering the current Claude, OpenAI, and Gemini families. Extend or override it with `~/.config/capybara/pricing.json`, which is merged over the built-in one. A model with no entry stays unpriced rather than being guessed at, and a rate that varies by context length or by date is recorded at its standard tier — the table has no conditions in it.

## Architecture

One Go binary holds the receiver, the store, the analysis pass, the TUI, and the web view. Storage is SQLite through `modernc.org/sqlite`, so there's no CGo and no database to run.

Spans arrive over OTLP or from a file and are normalised into one shape: a run, a tree of spans each with a kind (`agent`, `llm`, `tool`, `retrieval`), and the recorded content hanging off them. Everything downstream reads that shape, which is why a Claude Code session and an OpenTelemetry stream end up in the same views. Attributes that don't map are kept raw and shown under `a`.

Analysis is a background pass over spans not yet analysed, woken whenever the store is written. It's incremental and restart-safe: a re-imported span is analysed again, and findings carry a unique key so the second pass records nothing new. Cost is computed per LLM span from the recorded token counts, cache reads included where the source reports them.

The Python SDK is optional. It only produces OTLP spans, plus a declared schema per tool when you give it one, and the replay entry point the binary calls back into.

## License

[MIT](LICENSE)
