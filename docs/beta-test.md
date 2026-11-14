# Beta test: first trace and investigation

Install the current source with `go install github.com/tonquoc0407/capybara/cmd/capybara@main`; the latest published tag may have an older TUI. For a fresh local database, run `export CAPYBARA_DB=./beta.db`, then `capybara`.

Press `i` to open Connect. Press `t` to send a sample trace through the local HTTP receiver, then `o` to inspect it. To connect your app, choose a source and press `v` to read its full setup or Enter to copy it. Connect shows the next run your app sends.

Choose the input that matches your app:

- For an app already emitting OTLP HTTP/protobuf, point its exporter at `http://127.0.0.1:4318` and make one traced request. The [LangGraph and OpenInference examples](../demo/frameworks/) run against a local model stub.
- For Python, run `pip install capybara-sdk`, add `import capybara; capybara.init()`, and call a function wrapped with `@capybara.trace(tool="lookup_price")`. See the [Python SDK example](../README.md#from-your-own-agent).
- For Node, build and install the package using the [Node SDK README](../sdk-js/README.md). Set `const provider = init()`, call a function wrapped with `trace(fn, { tool: 'lookup_price' })`, then `await provider.forceFlush()` before a short process exits. The package is not published on npm yet.

In the TUI, select a span and press `tab` until **DETAIL** is focused. Press Space for full width, `l` for linked logs, `n`/`p` to move between logs, and `[`/`]` for long payload pages. The inspector shows the full run label, span name and IDs. `y` copies the selected output or log body. `a` shows raw span attributes.

To report a problem, run `capybara doctor -bundle diagnostics.json` and use the [beta feedback form](https://github.com/tonquoc0407/capybara/issues/new/choose). The bundle contains aggregate counts, version, OS and architecture. Review any additional screenshot or log before posting; recorded names, attributes and bodies can contain application data.
