# Record from the repository root: vhs demo/sdk-error.tape
# Runs a real Node SDK example with an in-memory OpenTelemetry exporter.
Output demo/sdk-error.gif

Set Shell bash
Set FontSize 18
Set Width 1100
Set Height 520
Set Padding 20
Set Framerate 24
Set TypingSpeed 30ms
Set WindowBar Colorful

Hide
Type "unset NO_COLOR; cd sdk-js && npm run build && clear" Enter
Sleep 2s
Show
Type "sed -n '9,20p' examples/error-span.mjs" Enter
Sleep 2s
Type "node examples/error-span.mjs" Enter
Sleep 3s
