# Run from the repository root after: go build -o /tmp/capybara ./cmd/capybara
Output demo/external/agents_issue_4070/showcase.gif
Output demo/external/agents_issue_4070/showcase.mp4

Set Shell "bash"
Set Theme "Dracula"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 19
Set Width 1300
Set Height 660
Set Framerate 12
Set TypingSpeed 4ms

Type "/tmp/capybara -db /tmp/capybara4070.db import demo/external/agents_issue_4070/trace.jsonl" Enter
Sleep 900ms
Type "/tmp/capybara -db /tmp/capybara4070.db runs" Enter
Sleep 2s
Type "/tmp/capybara -db /tmp/capybara4070.db diff trace_76d3 trace_451b" Enter
Sleep 3s
Type "/tmp/capybara -db /tmp/capybara4070.db export --golden -o /tmp/capybara4070-golden trace_76d3" Enter
Sleep 900ms
Type "/tmp/capybara -db /tmp/capybara4070.db check /tmp/capybara4070-golden/golden_trace_76.json trace_451b" Enter
Sleep 4s
