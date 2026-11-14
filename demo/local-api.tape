# Record from the repository root: vhs demo/local-api.tape
# A labelled synthetic failure arrives while Connect is open.
# The recording uses a throwaway database and disables session watching.
Output demo/pupu-tour.gif
Output demo/pupu-tour.mp4

Set Shell bash
Set FontSize 18
Set Width 1400
Set Height 780
Set Padding 24
Set Framerate 30
Set TypingSpeed 28ms
Set WindowBar Colorful

Hide
Type "unset NO_COLOR; REPO=$PWD; DEMO_DIR=$(mktemp -d); go build -o $DEMO_DIR/capybara ./cmd/capybara && export PATH=$DEMO_DIR:$PATH CAPYBARA_DB=$DEMO_DIR/demo.db CAPYBARA_OTLP_ADDR=127.0.0.1:43218 CAPYBARA_NO_CLAUDE=1 PS1='$ ' && printf '%s%s\\n' BUILD_ READY" Enter
Wait /BUILD_READY/
Type "(sleep 6; CAPYBARA_EVENTS_URL=http://127.0.0.1:43218/api/v1/events node $REPO/demo/local_api.mjs >$DEMO_DIR/send.log 2>&1) &" Enter
Type "capybara" Enter
Sleep 1s
Show
Sleep 1s
Type "i"
Sleep 1s
Type "t"
Sleep 1s
Type "v"
Sleep 1s
Escape
Sleep 7s
Sleep 1s
Type "o"
Sleep 1s
Down
Sleep 2s
Down
Sleep 2s
Type "w"
Sleep 2500ms
