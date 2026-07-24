# Records knightvision.gif: somebody else's LangChain agent, unmodified, traced
# live into capybara. KnightVision is a chess analytics project whose agent
# answers questions over a DuckDB warehouse of 9.4M Lichess games and a Chroma
# index; it is instrumented from the outside by OpenLLMetry and never edited.
#
# Needs the project checked out, its own virtualenv, and a key for the model it
# calls. Nothing here is capybara-specific:
#
#   export KNIGHTVISION=/path/to/KnightVision GOOGLE_API_KEY=...
#   vhs demo/knightvision.tape
#
# The agent is queued to start after the TUI is up, so the run arrives live. The
# key is only ever referenced by name, never printed.

Output demo/knightvision.gif

Set Shell bash
Set FontSize 16
Set Width 1400
Set Height 760
Set Padding 20
Set Framerate 24
Set TypingSpeed 45ms

Hide
Type "REPO=$PWD; mkdir -p /tmp/capybara-kv && go build -o /tmp/capybara-kv/capybara ./cmd/capybara" Enter
Sleep 45s
Type "export PATH=/tmp/capybara-kv:$PATH PS1='$ ' HOME=$(mktemp -d) && cd $HOME" Enter
Sleep 1s
Type "( sleep 8; cd $KNIGHTVISION && .venv/bin/python $REPO/demo/knightvision_run.py 'Which opening family has the highest white win rate, and how many games back it up?' >/dev/null 2>&1 ) &" Enter
Sleep 1s
Type "clear" Enter
Show

Type "capybara"
Sleep 400ms
Enter

# Empty at first, then somebody else's agent shows up: two Gemini turns either
# side of a DuckDB query, priced from the recorded token counts.
Sleep 26s

Down Down Down Down Down Down
Sleep 5s

Type "w"
Sleep 4s

Type "q"
Sleep 1s
