# Records frameworks.gif: a real LangGraph agent, traced by OpenLLMetry, caught
# answering past a failed tool. Run from the repository root, with the
# demo/frameworks requirements installed in .venv:
#
#   python -m venv .venv && .venv/bin/pip install -r demo/frameworks/requirements.txt
#   vhs demo/frameworks.tape
#
# The agent is scheduled to start a few seconds after the TUI opens, so the run
# arrives live rather than being imported afterwards. The recording runs under a
# throwaway HOME so the watcher finds no real Claude session directory and
# nothing private can land in the frame.

Output demo/frameworks.gif

Set Shell bash
Set FontSize 16
Set Width 1400
Set Height 760
Set Padding 20
Set Framerate 24
Set TypingSpeed 45ms

Hide
Type "REPO=$PWD; mkdir -p /tmp/capybara-fw && go build -o /tmp/capybara-fw/capybara ./cmd/capybara" Enter
Sleep 45s
Type "export PATH=/tmp/capybara-fw:$PATH PS1='$ ' HOME=$(mktemp -d) && cd $HOME" Enter
Sleep 1s
# The stub API stands in for the model provider, and the agent is queued to run
# eight seconds from now - after the TUI is up and listening.
Type "nohup $REPO/.venv/bin/python $REPO/demo/frameworks/fake_openai_api.py >/dev/null 2>&1 &" Enter
Type "( sleep 8; $REPO/.venv/bin/python $REPO/demo/frameworks/langchain_agent.py >/dev/null 2>&1 ) &" Enter
Sleep 1s
Type "clear" Enter
Show

Type "capybara"
Sleep 400ms
Enter

# Empty at first: the pane says what it is listening on. Then the agent runs and
# the trace arrives without anything being imported.
Sleep 14s

# Down to the turn that answered past the failed lookup.
Down Down Down Down Down Down Down Down Down Down Down Down Down Down
Sleep 6s

Type "q"
Sleep 1s
