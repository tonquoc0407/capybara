# Records demo.gif for the README. Run from the repository root: vhs demo/demo.tape
#
# session.jsonl is a real Claude Code session exported as structure only: tool
# names, timings and token counts, no prompts and no payloads. demo.jsonl is a
# scripted run that fails, which is what a finding looks like.
#
# The recording runs under a throwaway HOME so the watcher finds no real Claude
# session directory and nothing private can land in the frame.

Output demo/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1400
Set Height 760
Set Padding 20
Set Framerate 24
Set TypingSpeed 45ms

Hide
Type "REPO=$PWD; mkdir -p /tmp/capybara-demo && go build -o /tmp/capybara-demo/capybara ./cmd/capybara && export PATH=/tmp/capybara-demo:$PATH HOME=$(mktemp -d) PS1='$ ' && cd $HOME && clear" Enter
Sleep 45s
Show

Type "capybara import $REPO/demo/session.jsonl"
Sleep 400ms
Enter
Sleep 1800ms
Type "capybara import $REPO/demo/demo.jsonl"
Sleep 400ms
Enter
Sleep 1800ms

Type "capybara"
Sleep 400ms
Enter
Sleep 4s

# Down to the llm turn that answered with a price the failed call never
# returned: the detail pane shows the evidence next to the invented number.
Down Down Down Down Down
Sleep 4s

# Two tabs reach the run list the long way round, through the detail pane.
# Then down to the real session, and the cost waterfall over its 191 spans.
Tab
Sleep 400ms
Tab
Sleep 600ms
Down Down
Sleep 3s
Type "w"
Sleep 3500ms

Type "q"
Sleep 1s
