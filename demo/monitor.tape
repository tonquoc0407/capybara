# Records monitor.gif for the README. Run from the repository root:
#   vhs demo/monitor.tape
#
# Unlike demo.tape this needs a live process: the monitor draws resource
# samples, and a sample only exists while something is running. The pipeline in
# monitor_pipeline.py is killed inside its third node, so the recording ends on
# what a silent crash looks like - the graphs keep their readings, no span ever
# arrives for the node that died, and the orphaned_span finding lands about five
# seconds later, once the sampling gap is long enough to be a death.
#
# The recording stays inside the tui to the last frame: quitting first would end
# it on the shell, where the exporter's retry messages are the last thing shown.
#
# Runs under a throwaway HOME so the watcher finds no real Claude session.

Output demo/monitor.gif

Set Shell bash
Set FontSize 16
Set Width 1200
Set Height 800
Set Padding 20
Set Framerate 24
Set TypingSpeed 30ms

Hide
Type "REPO=$PWD; mkdir -p /tmp/capybara-monitor && go build -o /tmp/capybara-monitor/capybara ./cmd/capybara && export PATH=/tmp/capybara-monitor:$PATH HOME=$(mktemp -d) PS1='$ ' PIPE=$REPO/demo/monitor_pipeline.py PY=$REPO/sdk/.venv/bin/python && cd $HOME && clear" Enter
Sleep 45s
Show

# The pipeline runs in the background so the tui can watch it happen.
Type "$PY $PIPE &"
Sleep 300ms
Enter
Sleep 600ms

Type "capybara"
Sleep 300ms
Enter
Sleep 2500ms

# m opens the monitor. The span tree is still empty here: a span is only
# exported once it ends, while the graphs are already drawing.
Type "m"
Sleep 8s

# The heap climbs through the second node and is released when it returns.
Sleep 6s

# The third node is killed inside its span. Readings stop, and five seconds
# later that gap becomes an orphaned_span finding on the run.
Sleep 12s
