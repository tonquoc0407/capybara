# Security

Report a vulnerability through [GitHub Security Advisories](https://github.com/tonquoc0407/capybara/security/advisories/new)
rather than a public issue. Expect a response within a week.

Only the latest tagged release is supported.

Notes on capybara's own attack surface:

- `capybara import`, `serve`, and the OTLP receiver parse untrusted trace data by
  design (traces come from whatever agent you point at capybara). Malformed input
  should produce a finding or an error, never a crash.
- The judge commands (`faithfulness`, `toolcheck`, `relevance`) send run content to
  an external LLM endpoint. They are off by default and only activate when
  `--url`/`--model` or `CAPYBARA_JUDGE_*` env vars are set.
- `capybara.db` and replay manifests may contain full request/response bodies from
  your agent, including anything sensitive it saw. Treat them like logs.
- `capybara replay` executes the entrypoint recorded in the manifest. That
  manifest is data, not something capybara chose, so replaying a `capybara.db`
  or manifest file you did not produce yourself runs a binary someone else
  named. The command prints the resolved entrypoint before it runs.
- The `secret_leak` finding matches a fixed set of provider key formats (AWS,
  GitHub, OpenAI, and similar prefixes) plus a card-number check; it does not
  catch generic high-entropy strings, custom internal key formats, or bare
  `KEY=value` secrets. The `prompt_injection` finding matches a fixed list of
  English phrases; rephrasing, other languages, or splitting the phrase across
  spans bypasses it. Both are heuristics, not a guarantee.
