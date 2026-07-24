![capybara](demo/demo.gif)

capybara records what an AI agent did and shows you where it went wrong.

## Install

```
go install github.com/tonquoc0407/capybara/cmd/capybara@latest
```

Or take a binary from the [releases page](https://github.com/tonquoc0407/capybara/releases).
One file, no CGo, no runtime dependencies.

## Usage

Run it with no arguments. It opens the TUI, listens for OTLP on `localhost:4318`,
and tails `~/.claude/projects` when that directory exists:

```
capybara
```

Traces land in `capybara.db` in the working directory. Point somewhere else with
`-db`, and drop prompt and tool bodies with `-no-content`.

The tree marks `x` for a failed span, `!` for one carrying a finding. Findings are
recorded, never enforced — capybara does not fail your build. The one worth
knowing about is `improvised`: a tool call failed, and the next model turn
answered as if it had not.

```
tool get_stock_price x                    3.0s
  missing field: as_of (+3)
llm messages.create !                    17.0s
  improvised after get_stock_price failure
```

Other commands:

```
capybara import trace.jsonl     span-per-line jsonl, or agent-replay json
capybara watch claude           tail a session source without the TUI
capybara diff <run_a> <run_b>   align spans, mark the first divergence
capybara blame <run>            walk the final output back to its tainted source
capybara replay <run>           re-run a recording, optionally with an edited tool result
capybara export <run> --golden  write a CI fixture
capybara export <run> --html    write a self-contained page
capybara check <golden> <run>   compare against a golden, non-zero on divergence
capybara serve                  read-only web view
```

In the TUI: `j`/`k` to move, `enter` to expand, `/` to search, `f` to filter, `w`
for the cost waterfall, `c` for the context view, `d` to diff two runs, `b` to
blame, `r` to re-run from a span, `t` to export a test, `?` for help.

To record from your own agent, install the Python SDK:

```
pip install capybara-sdk
```

## Config

`~/.config/capybara/config.toml`:

```toml
theme = "bara"
```

`bara` is the default warm dark. `mono` drops the accent to grey; `paper` is for a
light terminal. Red and amber mean the same thing in all three.

## Architecture

One Go binary holds the OTLP receiver, the store, the analysis pass, the TUI and
the web view. Storage is SQLite through `modernc.org/sqlite`, so there is no CGo
and no database to run. Spans arrive over OTLP or from a file; a background pass
learns a JSON schema per tool from observed outputs and records drift against it.
The Python SDK is optional — it only produces OTLP spans.
