# Records demo.gif for the README. Run from the repository root: vhs demo/demo.tape
#
# The recording runs under a throwaway HOME so the watcher finds no real Claude
# session directory and nothing private can land in the frame.

Output demo/demo.gif

Set Shell bash
Set FontSize 16
Set Width 1280
Set Height 720
Set Padding 20
Set Framerate 24
Set TypingSpeed 45ms

Hide
Type "REPO=$PWD; mkdir -p /tmp/capybara-demo && go build -o /tmp/capybara-demo/capybara ./cmd/capybara && export PATH=/tmp/capybara-demo:$PATH HOME=$(mktemp -d) PS1='$ ' && cd $HOME && clear" Enter
Sleep 45s
Show

Type "capybara import $REPO/demo/demo.jsonl"
Sleep 500ms
Enter
Sleep 2s

Type "capybara"
Sleep 500ms
Enter
Sleep 3500ms

# Down to the llm turn that answered with a price the failed call never
# returned: the detail pane shows the evidence next to the invented number.
Down Down Down Down Down
Sleep 4s

# Cost waterfall, then back to the tree.
Type "w"
Sleep 2500ms
Type "w"
Sleep 1500ms

Type "q"
Sleep 1s
