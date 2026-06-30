# CLAUDE.md

## Project

dirstral-conformance is a language-agnostic, black-box conformance harness for
`dirstral-spec` compliant MCP servers (such as `dir2mcp`). It drives a server
**binary** over stdio or HTTP and asserts only externally-observable behavior —
it must never import an implementation's internals. The suite validates four
areas: lifecycle/session behavior, tool schema conformance, canonical error
behavior, and x402 request-gating mode behavior.

The server under test is supplied via the `DIR2MCP_BINARY` environment variable,
so the harness can be pointed at any spec-compliant binary rather than one
specific implementation.

## Repository layout

- `conformance/`: the Go test suite (package `conformance_test`)
  - `suite_test.go`: `TestMain` (binary lifecycle, gated on `DIR2MCP_BINARY`) plus
    the lifecycle / tools / errors / x402 test groups (currently stubs pending #105)
- `go.mod`: module `dirstral-conformance`, Go 1.24.2, no third-party dependencies
- `.github/workflows/go.yml`: "Conformance CI" — compile/stub validation on push/PR to `main`
- `.goreleaser.yml`: release metadata only; `builds: skip` (this is a test-only repo, it ships no binaries)
- `README.md`: scope and usage

## Build and test

There is no `Makefile` and no binary to build — this repo is purely a Go test suite.

- Compile-check the suite (what CI runs):
  ```bash
  go test ./conformance/... -run TestDoesNotExist -count=1
  ```
- Run the full conformance suite against a server binary:
  ```bash
  DIR2MCP_BINARY=/path/to/server-binary go test -v ./conformance/...
  ```
- Without `DIR2MCP_BINARY`, the suite still runs but tests skip/stub; this keeps
  CI green without a real server present.

Most tests are currently `t.Skip("stub: implement in #105")`; implement real
assertions in place rather than adding new packages.

## Working conventions

- Keep the harness implementation-agnostic: **never** import dir2mcp (or any
  server) internals; interact only through the spawned binary over stdio/HTTP.
- Keep all tests in the `conformance/` package; assert only externally-observable
  behavior (handshake, tool schemas, response shapes, error codes, x402 modes).
- Gate anything needing a live server on `DIR2MCP_BINARY`; the suite must still
  compile and pass (via skips) when the variable is unset, so CI stays green.
- Keep changes scoped to the issue; prefer deterministic, explicit assertions.
- No third-party dependencies unless clearly justified — `go.mod` is currently
  dependency-free.

## Known gotchas

- The server binary is selected by the `DIR2MCP_BINARY` env var; if it is unset,
  server-dependent tests are skipped, not failed.
- CI does **not** run real conformance assertions — it only proves the suite
  compiles (`-run TestDoesNotExist`), because no server binary is available in CI.
- `.goreleaser.yml` has `builds: skip`; do not expect release artifacts/binaries
  from this repo.
- x402 mode semantics asserted by the suite:
  - `off`: pass requests through with no payment headers
  - `on`: fail-open when the facilitator is unavailable
  - `required`: fail-closed when the facilitator is unavailable

## PR checklist

- [ ] `go test ./conformance/... -run TestDoesNotExist -count=1` compiles clean
- [ ] New/changed assertions verified against a real `DIR2MCP_BINARY` locally
- [ ] No imports of server/implementation internals introduced
- [ ] Server-dependent tests remain gated on `DIR2MCP_BINARY`
- [ ] `README.md` remains truthful
- [ ] No unrelated files changed
