#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")/.."

command -v vhs >/dev/null || { echo "Install VHS to record the demos" >&2; exit 1; }
command -v go >/dev/null || { echo "Install Go to build capybara for the demos" >&2; exit 1; }
command -v node >/dev/null && command -v npm >/dev/null || { echo "Install Node.js and npm to record the SDK demos" >&2; exit 1; }
[[ -x demo/frameworks/.venv/bin/python ]] || { echo "Set up demo/frameworks/.venv first" >&2; exit 1; }
[[ -x sdk/.venv/bin/python ]] || { echo "Set up sdk/.venv first" >&2; exit 1; }

for tape in \
  demo/local-api.tape \
  demo/demo.tape \
  demo/frameworks.tape \
  demo/monitor.tape \
  demo/external/agents_issue_4070/showcase.tape \
  demo/sdk-error.tape; do
  vhs "$tape"
done
