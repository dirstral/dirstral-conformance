# AGENTS.md

## Purpose

Operational guide for coding agents working in this repository.

## Before you start

- Re-check whether the requested plan still matches the current codebase before making changes.
- Review relevant context first: `README.md`, the canonical `dirstral-spec` docs
  (the spec this harness verifies against), and the affected files under
  `conformance/`.
- Preserve the black-box, implementation-agnostic design: the harness drives a
  server binary and asserts only externally-observable behavior.

## Project summary

dirstral-conformance is a language-agnostic, black-box conformance harness for
`dirstral-spec` compliant MCP servers. It launches a server binary (chosen via
the `DIR2MCP_BINARY` env var) over stdio or HTTP and asserts lifecycle/session
behavior, tool schema conformance, canonical error behavior, and x402 request-
gating mode behavior. It never imports server internals.

## Repo map

- `conformance/` - the Go test suite (package `conformance_test`)
- `conformance/server_test.go` - launches the binary under test and waits for its listening URL
- `conformance/client_test.go` - stdlib MCP client over streamable HTTP
- `conformance/suite_test.go` - fixture corpus + lifecycle, tools/list, session and error assertions
- `conformance/answer_provenance_test.go` - SPEC 9.4.5
- `go.mod` - module `dirstral-conformance`, Go 1.24.2, no dependencies
- `.github/workflows/go.yml` - "Conformance CI": builds the reference implementation from source and RUNS the suite against it
- `.goreleaser.yml` - release metadata only (`builds: skip`; no binaries shipped)
- `README.md` - scope and usage

## Build / test / CI commands

There is no `Makefile` and nothing to compile into a binary — this is a test-only repo.

Compile-check all packages (CI "Build" step):

```bash
go test ./... -run TestDoesNotExist
```

Compile-check the conformance suite (CI "Suite compile" step):

```bash
go test ./conformance/... -run TestDoesNotExist -count=1
```

Run the full conformance suite against a server binary:

```bash
DIR2MCP_BINARY=/path/to/server-binary go test -v ./conformance/...
```

Notes:

- With no `DIR2MCP_BINARY`, the suite compiles and skips. That is NOT a pass:
  `TestHarness_RefusesToPassVacuously` logs that the run asserted nothing about
  any server. This repo spent six months reporting green on eleven stubs, so
  treat a skipped run as an absent result.
- CI builds the reference implementation from source and runs the suite against
  it, so the gate tracks that implementation's `main` rather than its last
  release. A second implementation adds a build step and reuses the run.

## Conventions

- Work only in this repository unless explicitly instructed otherwise.
- **Never** import dir2mcp or any server's internals; interact only through the
  spawned binary over stdio/HTTP. This rule is the whole point of the harness.
- Keep all tests in the `conformance/` package and assert only externally-
  observable behavior (handshake, tool schemas, response shapes, canonical error
  codes, x402 modes).
- Gate anything requiring a live server on `DIR2MCP_BINARY`; preserve the
  "compiles and passes without a binary" property.
- Keep `go.mod` dependency-free unless a new dependency is clearly justified.
- Keep patches minimal and issue-focused; do not add extra markdown files unless
  the task explicitly requires them.
- Use Conventional Commits (<https://www.conventionalcommits.org/>) and do not
  mention yourself in commit messages.
- Do not push directly to `main`; open a PR.

### x402 mode behavior (asserted by the suite)

- `off` = pass through, no payment headers
- `on` = fail-open when the facilitator is unavailable
- `required` = fail-closed when the facilitator is unavailable
