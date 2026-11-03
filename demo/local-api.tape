# Record from the repository root: vhs demo/local-api.tape
# A labelled synthetic failure is sent through the real local HTTP listener.
# -no-claude keeps private sessions out of the recording.
Output demo/local-api.gif
Output demo/local-api.mp4

Set Shell bash
Set FontSize 18
Set Width 1400
Set Height 780
Set Padding 24
Set Framerate 30
Set TypingSpeed 28ms
Set WindowBar Colorful

Hide
Type "REPO=$PWD; DEMO_DIR=$(mktemp -d); GOCACHE=/tmp/capybara-go-cache go build -o $DEMO_DIR/capybara ./cmd/capybara && printf '%s%s\\n' BUILD_ READY" Enter
Wait /BUILD_READY/
Type "$DEMO_DIR/capybara -db $DEMO_DIR/demo.db -otlp 127.0.0.1:43218 listen >$DEMO_DIR/listen.log 2>&1 & LISTENER_PID=$!" Enter
Sleep 1s
Type "CAPYBARA_EVENTS_URL=http://127.0.0.1:43218/api/v1/events node $REPO/demo/local_api.mjs && printf '%s%s\\n' SEND_ READY" Enter
Wait /SEND_READY/
Type "kill $LISTENER_PID; wait $LISTENER_PID; clear" Enter
Sleep 1s
Show
Type "$DEMO_DIR/capybara -db $DEMO_DIR/demo.db -otlp 127.0.0.1:43218 -no-claude" Enter
Sleep 2s
Type "i"
Sleep 2s
Escape
Sleep 1s
Down
Sleep 2s
Down
Sleep 2s
Type "w"
Sleep 2500ms
Type "q"
Sleep 500ms
