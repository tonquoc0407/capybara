# Verification

Core behavior and migrations:

```sh
go test ./...
node --test internal/web/app_behavior_test.mjs
```

Tests that open HTTP/gRPC listeners need permission to bind localhost. The lightweight Web tests exercise navigation, lazy evidence loading, exact-text editing and live draft preservation without a browser dependency.

## Real browser

The optional check starts an authenticated hub with a temporary database and synthetic sample trace. It exercises Overview, live draft preservation, expected-answer persistence, dataset freeze/clone, prompt escaping, trace evidence and narrow-screen layout. It writes screenshots and the temporary database to the artifact directory printed on completion or failure.

One isolated setup, tested with Playwright 1.64.1:

```sh
npm install --prefix /tmp/capybara-browser-check --no-audit --no-fund playwright@1.64.1
PLAYWRIGHT_BROWSERS_PATH=/tmp/capybara-playwright-browsers \
  node /tmp/capybara-browser-check/node_modules/playwright/cli.js install chromium --only-shell
go build -o /tmp/capybara-web-e2e ./cmd/capybara
CAPYBARA_PLAYWRIGHT=/tmp/capybara-browser-check/node_modules/playwright/index.mjs \
PLAYWRIGHT_BROWSERS_PATH=/tmp/capybara-playwright-browsers \
  node scripts/browser_check.mjs /tmp/capybara-web-e2e
```

These are development tools; the capybara binary does not need Node, Playwright or a frontend build step. Browser automation does not establish real-user task completion rates or replace the planned independent usability study.

## Performance

The reproducible workload scripts and measured conditions are recorded in [the ingest baseline](benchmarks/2026-10-18.md) and [the operations dashboard baseline](benchmarks/2026-10-19.md). Measurements use synthetic data; each report lists the excluded work and host limits.
