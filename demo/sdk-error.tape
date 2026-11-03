# Record from the repository root: vhs demo/sdk-error.tape
# Runs a real Node SDK example with an in-memory OpenTelemetry exporter.
Output demo/sdk-error.gif

Set Shell bash
Set FontSize 18
Set Width 1160
Set Height 630
Set Padding 24
Set Framerate 24
Set TypingSpeed 22ms

Hide
Type "cd sdk-js && npm run build && clear" Enter
Sleep 2s
Show
Type "sed -n '9,20p' examples/error-span.mjs" Enter
Sleep 2s
Type "node examples/error-span.mjs" Enter
Sleep 3s
