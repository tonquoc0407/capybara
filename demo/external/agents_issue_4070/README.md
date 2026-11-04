# OpenAI Agents SDK issue 4070

This is a reproducible public bug from [openai-agents-python issue #4070](https://github.com/openai/openai-agents-python/issues/4070). On v0.19.1, an uncaught model error marks the generation span as failed. A regular agent run leaves its agent span marked successful; a streamed run marks that agent span as failed. The issue has since been closed, so the reproducer pins the affected version.

`trace.jsonl` contains three runs captured from that version: regular, streamed, and a regular agent that calls a tool before the model fails. The original issue demonstrates the single model failure; we extended it with the upstream test helpers to exercise a complete agent → model → tool → model pipeline. The model and tool are deterministic, so reproduction needs no API key or paid call. These are real SDK traces from a public bug reproducer, not production user traffic.

With `capybara` installed, inspect the included trace from the repository root:

```sh
export CAPYBARA_DB=/tmp/capybara-issue4070.db
cd demo/external/agents_issue_4070
capybara import trace.jsonl
capybara runs
capybara findings
```

Expected with the fixed capybara source: all three runs show `error` and zero findings. Each run has a failed generation span. The streamed run also has a failed agent span. Run status now surfaces a terminal model failure even when an outer `task` span says `ok`; a recovered tool failure does not turn the whole run red. The upstream SDK bug is separate.

Compare the two single-turn runs and exercise the golden check:

```sh
capybara diff trace_76d3 trace_451b
capybara export --golden -o /tmp/capybara-issue4070-golden trace_76d3
capybara check /tmp/capybara-issue4070-golden/golden_trace_76.json trace_451b
```

`diff` prints `status ok -> error` on the agent span. `check` reports that span outcome and exits 1 even though there are no completed tool calls. The exported golden includes run and span statuses; older tool-only fixtures remain readable.

The [GIF](showcase.gif) and [MP4](showcase.mp4) show the import, run list, diff and CI check. To render again from the repository root, install [VHS](https://github.com/charmbracelet/vhs) and run:

```sh
vhs demo/external/agents_issue_4070/showcase.tape
```

To regenerate the trace from the affected SDK:

```sh
CAPYBARA_DIR="$PWD"
git clone --depth 1 --branch v0.19.1 https://github.com/openai/openai-agents-python.git /tmp/openai-agents-v0.19.1
cd /tmp/openai-agents-v0.19.1
uv run --no-dev --frozen python "$CAPYBARA_DIR/demo/external/agents_issue_4070/repro.py" . /tmp/issue4070.jsonl
```

`repro.py` uses the pinned repository's own `FakeModel` and trace processor. It converts their exported spans to capybara's generic JSONL import format; capybara does not natively parse the OpenAI Agents SDK export format. The original repository is MIT licensed.
