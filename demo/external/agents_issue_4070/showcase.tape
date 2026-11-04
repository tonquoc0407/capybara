# Record from the repository root: vhs demo/external/agents_issue_4070/showcase.tape
Output demo/external/agents_issue_4070/showcase.gif
Output demo/external/agents_issue_4070/showcase.mp4

Set Shell "bash"
Set FontSize 18
Set Width 1100
Set Height 520
Set Padding 20
Set Framerate 24
Set TypingSpeed 30ms
Set WindowBar Colorful

Hide
Type "unset NO_COLOR; REPO=$PWD; DEMO_DIR=$(mktemp -d); go build -o $DEMO_DIR/capybara ./cmd/capybara && cp $REPO/demo/external/agents_issue_4070/trace.jsonl $DEMO_DIR/ && export PATH=$DEMO_DIR:$PATH CAPYBARA_DB=$DEMO_DIR/traces.db PS1='$ ' && cd $DEMO_DIR && printf '%s%s\\n' BUILD_ READY" Enter
Wait /BUILD_READY/
Type "clear" Enter
Show
Type "capybara import trace.jsonl" Enter
Sleep 900ms
Type "capybara runs" Enter
Sleep 2s
Type "capybara diff trace_76d3 trace_451b" Enter
Sleep 3s
Type "capybara export --golden -o golden trace_76d3" Enter
Sleep 900ms
Type "capybara check golden/golden_trace_76.json trace_451b" Enter
Sleep 4s
