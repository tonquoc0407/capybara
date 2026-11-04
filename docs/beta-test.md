# Beta test: first trace and investigation

Install the current source with `go install github.com/tonquoc0407/capybara/cmd/capybara@main`; the latest published tag may have an older TUI. Use a fresh local database for the test: run `export CAPYBARA_DB=./beta.db`, then `capybara`. Press `i`, choose a source, then press `v` to read the full setup or Enter to copy it. The Connect screen confirms when a new run arrives; press `o` to open that run. For an immediate receiver check, choose **HTTP JSON events** and run its copied `curl` command in another terminal.

Three application paths:

1. **Existing OTLP instrumentor.** Point its OTLP HTTP/protobuf exporter at `http://127.0.0.1:4318`, make one traced request, and check the Connect screen. The [LangGraph and OpenInference examples](../demo/frameworks/) provide runnable instrumented apps with a local model stub.
2. **Python SDK.** `pip install capybara-sdk`, then add `import capybara; capybara.init()` before calling a function wrapped with `@capybara.trace(tool="lookup_price")`. Run the function once. The [Python SDK example](../README.md#from-your-own-agent) shows the wrapper.
3. **Node SDK.** Build and install the package from this checkout using the commands in the [Node SDK README](../sdk-js/README.md). Call `init()` and a function wrapped with `trace(fn, { tool: 'lookup_price' })`, then `await provider.forceFlush()` before a short process exits. The package is not published on npm yet.

In the TUI, select a span and press `tab` until **DETAIL** is focused. Press Space for full width, `l` for linked logs, `n`/`p` to move between logs, and `[`/`]` for long payload pages. The inspector shows the full run label, span name and IDs. `y` copies the selected output or log body. `a` shows raw span attributes.

To report a problem, run `capybara doctor -bundle diagnostics.json` and use the [beta feedback form](https://github.com/tonquoc0407/capybara/issues/new/choose). The bundle contains aggregate counts, version, OS and architecture. Review any additional screenshot or log before posting; recorded names, attributes and bodies can contain application data.
