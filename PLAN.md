# Capybara: portability and long-term upgrades

This is the working roadmap, not a claim that every target already works.
Keep it updated after each scoped patch, with exact checks and remaining gaps.

## Direction and guardrails

- Keep the Go CLI/TUI, local SQLite database, OTLP compatibility, and single
  CGo-free executable. Do not rewrite the project or add a desktop wrapper by
  default.
- Target modern Linux, macOS, and Windows on x86-64 and ARM64. Publish minimum
  OS/kernel and terminal requirements after runtime testing; do not promise
  every historical Linux distribution or CPU.
- Test glibc and musl Linux: Ubuntu/Debian, Fedora, Arch, Alpine, and NixOS are
  representative targets. Treat WSL as Linux, not evidence for native Windows.
- Preserve existing databases, CLI defaults, and user configuration. Prefer
  explicit overrides and backwards-compatible reads over silent migration.
- Keep collection and analysis local by default. External LLM judging and live
  replay must remain explicit. Do not expose the receiver/web UI publicly by
  default.
- Benchmark before optimizing. Avoid new services, dependencies, or refactors
  unless a concrete portability or performance need justifies them.
- Repository changes are local until reviewed. Package-manager submissions,
  signing credentials, publishing, and remote changes require separate approval.

## Starting point

- `.goreleaser.yaml` already declares Linux/macOS/Windows builds for amd64/arm64,
  with `CGO_ENABLED=0`, archives, and Sigstore-signed checksums.
- Go CI already declares tests and lint on Ubuntu, macOS, and Windows.
- Python and Node SDK CI initially only ran on Linux.
- Config lookup is duplicated and uses Linux-style paths on every OS.
- Editor selection initially splits on whitespace and defaults to `vi`.
- Python memory sampling initially uses Linux `/proc`, macOS peak RSS, and no
  Windows RSS. GPU sampling is optional NVIDIA-only, device-wide, first card.
- Replay is Python SDK-specific; arbitrary OTLP and Node recordings are not
  automatically replayable. Some process tests use Unix stand-ins or skip Windows.

## Phase 1 — Cross-platform foundation (current)

- [x] Share configuration-path resolution across theme, pricing, mapping, and
  the Claude-watcher notice marker.
  - Honor `XDG_CONFIG_HOME` on all targets.
  - Use native config roots for new macOS/Windows installs.
  - Keep per-file reads from `~/.config/capybara` when no native file exists;
    never move/delete old files automatically. Explicit XDG override is authoritative.
- [x] Fix editor selection and errors.
  - Preserve `VISUAL` > `EDITOR`; use `notepad.exe` on Windows and `vi` elsewhere.
  - Support quoted executable/argument paths and retain Windows backslashes.
  - Reject empty/malformed commands without panicking or invoking a shell.
- [x] Report current process RSS consistently on Linux/macOS/Windows.
  - Use `psutil` in the Python SDK rather than bespoke OS API bindings.
  - Reuse a process handle; fail gracefully if memory measurement is unavailable.
  - Test attribution, live RSS, and failure paths. GPU availability stays optional.
- [x] Configure Python and Node SDK CI on Ubuntu/macOS/Windows; retain Python 3.10 and
  3.13 coverage. Deterministic tests must not depend on a host editor/GPU.
- [ ] Obtain passing native CI results for the expanded SDK matrix.
- [x] Document config precedence, editor command syntax, and monitoring limits.
- [x] Harden editor integration tests so success cannot mean a subprocess never
  ran, and remove the Windows skip. Replay's Unix-only stand-ins remain tracked.
- [ ] Harden remaining process/replay integration tests so success cannot mean a
  subprocess never ran; replace inappropriate Unix-only fixtures.
- [ ] Validate native ARM64 execution where runners/hardware permit, in addition
  to six-target cross-compilation. Verify terminal resize, Ctrl+C, Unicode,
  filesystem watching, file locks, paths with spaces, and non-ASCII paths.

**Exit criteria:** Go tests/lint and SDK tests/typechecks pass on the native OS
matrix; six release targets compile without CGo; legacy config behavior is
covered. Cross-compilation is not a substitute for native runtime tests.

## Phase 2 — Diagnostics and deployment

- [ ] Add `capybara doctor`: OS/arch/build, config/db paths, writable locations,
  terminal capabilities, listener conflicts, and optional Python/editor/GPU
  availability. Do not print recorded content or credentials.
- [x] Add headless collection with graceful shutdown and clear listener status.
- [ ] Provide explicit controls for automatic Claude-session watching and custom
  session roots; preserve the current default until a documented change is agreed.
- [ ] Add distro smoke tests for import/analyze/export/check and headless OTLP
  intake, including Alpine and NixOS. Document supported minimum versions.
- [ ] Test restart, log rotation, watcher limits, busy databases, and interrupted
  writes; offer a polling fallback only if native watcher failures justify it.

**Exit criteria:** a fresh install can diagnose its environment and collect
without a TTY; representative Linux smoke tests and shutdown/restart tests pass.

## Phase 3 — Packaging and release trust

- [ ] Keep binary archives + checksums as the universal installation path.
- [ ] Add Debian/RPM packaging with installation/uninstallation smoke tests.
- [ ] Add a project-local pinned Nix development shell and validate on NixOS.
- [ ] Retain Homebrew; plan Apple signing/notarization for macOS binaries.
- [ ] Document PowerShell installation/checksum verification; then evaluate
  Scoop/winget submissions and optional Windows executable signing.
- [ ] Verify release archive contents and checksum instructions on each OS.

**Exit criteria:** repeatable local packaging checks and documented installs on
all targets. Sigstore checksum signing is not Apple notarization or Windows
executable signing. Publishing and signing depend on maintainer authorization.

## Phase 4 — Measure and optimize large traces

- [ ] Add reproducible fixtures/benchmarks for 1k/10k/100k spans, burst OTLP,
  large tool outputs, resource samples, and repeated imports.
- [ ] Capture import/analysis duration, allocations/peak memory, database growth,
  receiver throughput, UI response/refresh latency, and SDK overhead.
- [ ] Define regression budgets from measured baselines, not guessed numbers.
- [ ] Optimize demonstrated bottlenecks: bounded analysis batches, paginated run
  lists, indexes/query plans, and coalesced refreshes as needed.
- [ ] Verify finding identity, ordering, schema learning, replay, and restart
  safety remain correct under batching/concurrency changes.

**Exit criteria:** published benchmark commands and before/after results; no
correctness/corpus regressions; keep SQLite unless evidence warrants otherwise.

## Phase 5 — Privacy and replay safety

- [ ] Add configurable content redaction at ingest (including raw attributes),
  with explicit limits and tests for every ingest source.
- [ ] Add retention/pruning controls for spans/content/resource samples, with
  dependency-aware deletion, dry runs, and database maintenance guidance.
- [ ] Audit `-no-content`: test all places prompt/tool bodies may persist,
  including raw attributes and caches. Distinguish dropping content from encryption.
- [ ] Make replay runtime requirements and unsupported recordings explicit.
- [ ] Display the executable/working directory and obtain intentional consent
  before launching entrypoints from untrusted recordings.
- [ ] Clearly indicate when an edited replay becomes live and may incur cost or
  tool side effects. Recorded-response interception is not an OS sandbox.

**Exit criteria:** privacy behavior and replay trust boundaries are documented
and tested; default behavior sends no content to new external services.

## Phase 6 — Debugger UX and product capabilities

- [ ] Improve large-run search/filtering/navigation and actionable finding detail.
- [ ] Improve the web viewer incrementally, keeping content rendered as text and
  localhost/read-only defaults; evaluate live updates without duplicating analysis.
- [ ] Improve comparison, exports, and CI-baseline workflows from real user cases.
- [ ] Evaluate Node replay/resource monitoring and more GPU backends separately,
  with clear runtime support and graceful unavailable states.
- [ ] Use `ui-ux-pro-max` for substantial visual/accessibility work; verify small
  terminals, light/dark themes, keyboard operation, and non-color status cues.

**Exit criteria:** each UX change has a concrete use case, interaction tests,
and a visual/accessibility review; no mandatory browser/desktop runtime for CLI use.

## Verification playbook

Run the smallest relevant tests first, then:

```sh
export CGO_ENABLED=0  # Match the release binary; these examples use a POSIX shell.
go test ./...
go vet ./...
# When available: golangci-lint run
sh corpus/run.sh
cd sdk && uv sync --extra dev --locked && uv run pytest -q && uv run mypy
cd ../sdk-js && npm ci && npm run build && npm run typecheck && npm test
```

Also run Python lint/format checks, review `git diff --check`, and build
`linux`, `darwin`, `windows` × `amd64`, `arm64` with `CGO_ENABLED=0`.
Use native CI/manual runs for OS API and terminal behavior. Record checks that
could not run, rather than treating them as passed.

## Progress log

### 2026-09-17 — Initial foundation work

- Roadmap created; config resolution, editor parsing, SDK RSS sampling, and CI
  portability coverage are now implemented in the foundation patch.
- Local host is Linux/NixOS. Go/uv/ruff obtained via an ephemeral `nix shell`;
  no global installer/configuration changes needed.
- Verified locally: CGo-free Go tests, Python tests/mypy, Node build/typecheck/
  tests, and Linux/darwin/windows amd64/arm64 cross-builds.
- Exact checks on Linux/NixOS:
  - `CGO_ENABLED=0 go test ./...` and `CGO_ENABLED=0 go vet ./...`: passed;
    editor/config tests rerun after final test-fixture changes.
  - `CGO_ENABLED=0 golangci-lint run`: 0 issues;
    `golangci-lint fmt --diff`: no formatting differences.
  - `actionlint .github/workflows/ci.yml`: passed.
  - `CGO_ENABLED=0 sh corpus/run.sh`: all scored types at precision/recall/F1 1.0.
  - Python 3.13: locked dependency sync, 56 SDK tests, strict mypy, and 66
    exporter-script tests passed.
  - `ruff check .` and `ruff format --check .`: passed with Nix-packaged Ruff.
    The wheel-installed Ruff executable could not launch on NixOS (generic
    Linux loader); no global loader workaround was installed.
  - Node 24: build, typecheck, and all 11 tests passed.
  - Six CGo-free `go build -trimpath` targets passed; editor/config test binaries
    also cross-compiled for macOS/Windows ARM64. Artifacts stayed in `/tmp`.
  - Native Linux release-style binary: version, demo import, run listing, and
    findings smoke checks passed, using a temporary database/config directory.
  - `git diff --check`: passed.
- Added runtime dependency: `psutil>=5.9`; `types-psutil` is development-only.
  Locked psutil has wheels for the principal targets, including musl Linux;
  installation on platforms without a compatible wheel may need build tools.
- Native macOS/Windows and CI execution cannot be verified on this host. Keep
  those as required follow-up evidence, especially terminal behavior, native
  filesystem watchers, and SDK wheels/install behavior.
- Next patch: replace replay's misleading/Unix-specific process stand-ins with
  native subprocess tests, then implement `doctor` and headless collection.
