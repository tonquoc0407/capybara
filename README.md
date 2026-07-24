<img src="demo/logo.png" alt="" width="120" align="left">

# capybara

A terminal trace debugger for AI agents. It records what an agent did and shows
you where it went wrong.

<br clear="left">

![capybara](demo/demo.gif)

## Install

```
go install github.com/tonquoc0407/capybara/cmd/capybara@latest
```

Or take a binary from the [releases page](https://github.com/tonquoc0407/capybara/releases).
One file, no CGo, no runtime dependencies.

## Getting a trace in

Run it with no arguments. It opens the TUI, listens for OTLP on `127.0.0.1:4318`
and `127.0.0.1:4317`, and tails `~/.claude/projects` when that directory exists:

```
capybara
```

Traces land in `capybara.db` in the working directory. Point somewhere else with
`-db`, and drop prompt and tool bodies with `-no-content`. With an empty
database the middle pane says what it is listening on and how to send it
something, so there is nothing to look up.

If something else already holds 4317 or 4318 — a collector, Jaeger, another
tracing UI — capybara keeps the transport that did bind, says which one it lost,
and carries on. `-otlp 127.0.0.1:4319` moves the http listener somewhere free.

There are three ways in, and one database can hold all of them:

- **OTLP.** Point any instrumented app at it:
  `OTEL_EXPORTER_OTLP_ENDPOINT=http://127.0.0.1:4318`. Three attribute
  conventions are read — OpenTelemetry's own `gen_ai.*`, OpenInference
  (Arize/Phoenix), and OpenLLMetry (traceloop) — so the instrumentor you already
  run is enough, whichever one it is.
- **A session file.** `capybara watch claude` tails Claude Code's own logs. No
  instrumentation and no restart: it reads what is already on disk.
- **A file you have.** `capybara import trace.jsonl` takes span-per-line jsonl,
  or agent-replay json when the name ends in `.json`.

To record from your own agent, install the Python SDK:

```
pip install capybara-sdk
```

```python
import capybara

capybara.init()  # export to a local capybara on 127.0.0.1:4318

@capybara.trace(tool="lookup_price")
def lookup_price(sku: str) -> dict:
    return {"price": 42.0, "currency": "USD"}
```

`init()` reuses a `TracerProvider` you already configured, so an instrumentor you
already run keeps working; capybara only adds an exporter to it.

## Reading a trace

The tree marks `x` for a failed span and `!` for one carrying a finding. The run
column says when each run started, what it cost, and what was recorded against
it.

```
j/k     move            w   cost waterfall      d   diff two runs
enter   expand          c   context view        b   blame the output
/       search          f   filter by kind      r   re-run from a span
tab     change pane     a   raw attributes      t   export a pytest case
?       full help       e   edit a tool output  q   quit
```

The waterfall sorts spans by what they cost, so the turn that spent the money is
the first line rather than something to scroll for. The context view shows what
filled each turn's window — system text, tool output, history — and marks the
turns where it dropped, which is where a compaction ate something.

## What it looks for

Findings are recorded, never enforced: capybara does not fail your build. Six
kinds, all passive.

**`improvised`** is the one worth knowing about. A tool call failed, and the next
model turn answered as if it had not:

```
tool get_stock_price x                    3.0s
  missing field: as_of (+3)
llm messages.create !                    17.0s
  improvised after get_stock_price failure
```

The evidence sits beside the answer, so the invented number is on screen next to
the call that never returned it. A turn that retries the tool, or that says in
any of several languages that it failed, is not flagged.

**`drift`** fires when a tool's output stops matching the shape it has been
returning — a field disappears, or changes type. capybara learns that shape from
what it observes, one call at a time, and adopts the new one at the change point.
A tool that sometimes prints text and sometimes prints JSON has both accepted:
one line of JSON out of a shell command is not a contract change.

**`malformed`** and **`empty_payload`** cover output that will not parse, or that
arrived empty, and only for tools whose output has always been structured.

**`loop`** marks the same call repeated back to back with the same arguments — a
`Read` over ten files is a plan, the same `Read` ten times is a loop. Calls whose
arguments were never recorded are not compared against each other, because
nothing is known about them.

**`cost_spike`** marks a turn burning several times the run's own rolling
baseline.

If your tool has a declared schema, give it to the SDK with
`capybara.schema("lookup_price", Price)` and the declared shape wins over the
learned one, so the first violating call is a finding instead of a new version.

## Other commands

```
capybara watch claude           tail a session source without the TUI
capybara diff <run_a> <run_b>   align spans, mark the first divergence
capybara blame <run>            walk the final output back to its tainted source
capybara replay <run>           re-run a recording, optionally with an edited tool result
capybara export <run>           write a pytest case for the failure
capybara export <run> --golden  write a CI fixture
capybara export <run> --html    write a self-contained page
capybara check <golden> <run>   compare against a golden, non-zero on divergence
capybara serve                  read-only web view
```

`replay` serves the recorded model responses and tool outputs back to the agent
process, so nothing touches the network. Edit one tool result first and only the
turns after it go live: that is how you ask what the agent would have done with
the answer it should have got. A call that is not in the recording stops the
replay rather than running live.

`serve` and `export --html` render the same read-only page, one from the database
and one with a single run inlined. Recorded bodies are written as text, never as
markup.

## Config

`~/.config/capybara/config.toml`:

```toml
theme = "bara"
```

`bara` is the default warm dark. `mono` drops the accent to grey; `paper` is for
a light terminal. Red and amber mean the same thing in all three.

Model rates live in a table built into the binary. Extend or override it with
`~/.config/capybara/pricing.json`, which is merged over the built-in one. A model
with no entry stays unpriced rather than being guessed at.

## Architecture

One Go binary holds the receiver, the store, the analysis pass, the TUI and the
web view. Storage is SQLite through `modernc.org/sqlite`, so there is no CGo and
no database to run.

Spans arrive over OTLP or from a file and are normalised into one shape: a run,
a tree of spans each with a kind (`agent`, `llm`, `tool`, `retrieval`), and the
recorded content hanging off them. Everything downstream reads that shape, which
is why a Claude Code session and an OpenTelemetry stream end up in the same
views. Attributes that do not map are kept raw and shown under `a`.

Analysis is a background pass over spans not yet analysed, woken whenever the
store is written. It is incremental and restart-safe: a re-imported span is
analysed again, and findings carry a unique key so the second pass records
nothing new. Cost is computed per llm span from the recorded token counts, cache
reads included where the source reports them.

The Python SDK is optional. It only produces OTLP spans, plus a declared schema
per tool when you give it one, and the replay entry point the binary calls back
into.
