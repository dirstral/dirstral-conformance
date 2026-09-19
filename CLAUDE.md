# CLAUDE.md

## Project

dirstral-conformance is a language-agnostic, black-box conformance harness for
`dirstral-spec` compliant MCP servers. It launches a server **binary**, speaks
MCP to it over streamable HTTP, and asserts only externally-observable
behaviour. It must never import an implementation's internals.

## Why this repo exists, and why the work does not belong in dir2mcp

`dirstral-spec` mandates it. From `spec/versioning.md`, the breaking-change
process:

> 5. `dirstral-conformance` must add a new test suite for the new behavior

dir2mcp has its own in-tree conformance suite under `tests/conformance/`, and
it is good. It is also Go-package-scoped, so it can only ever validate dir2mcp.
The spec's compatibility matrix names more than one implementation. Only a
harness that drives a binary can hold all of them to one contract.

Read that history before proposing to simplify this repo away: it spent six
months as eleven skipped stubs because the harness work was done in dir2mcp
instead and nothing came back here.

## Repository layout

- `conformance/` - the Go test suite (package `conformance_test`)
  - `server_test.go` - launches the binary under test, waits for the listening
    URL on its NDJSON stdout, scrubs provider credentials from its environment
  - `client_test.go` - a stdlib MCP client: JSON-RPC over streamable HTTP, SSE
    events parsed per event and correlated by id
  - `suite_test.go` - the fixture corpus plus lifecycle, tools/list,
    session-recovery and error-taxonomy assertions
  - `answer_provenance_test.go` - SPEC 9.4.5
  - `schema_test.go` - helpers for reading an advertised tool schema
- `go.mod` - module `dirstral-conformance`, Go 1.24.2, **no dependencies**
- `.github/workflows/go.yml` - builds the reference implementation from source
  and runs the suite against it
- `.goreleaser.yml` - release metadata only (`builds: skip`)

## Build and test

There is no `Makefile` and no binary to build: this repo is a Go test suite.

```bash
# Against a server. This is the only run that asserts anything.
DIR2MCP_BINARY=/path/to/server go test ./conformance/... -v

# Compile-check only.
go test ./conformance/... -run TestDoesNotExist -count=1
```

## Conventions

- **Never let a test pass for the wrong reason.** This repo's own history is
  the cautionary tale: eleven `t.Skip` stubs reported the same green as a
  passing suite for six months. `TestHarness_RefusesToPassVacuously` exists to
  say out loud when a run asserted nothing, and every test that loops over
  surfaces fails when it found none.
- A contract that a client cannot enforce is not tested by asserting the words
  appear. Decode the structure and assert the relationship.
- No third-party dependencies. A harness built on an SDK tests the SDK's idea
  of the protocol rather than the wire contract.
- The startup flag set IS the portability contract. An implementation that
  accepts `--config` and `up --dir --state-dir --foreground --json
  --non-interactive --read-only --auth --listen`, and announces its URL as
  NDJSON on stdout, can be held to this suite. Changing those flags changes
  what a second implementation must provide, so change them deliberately.
- The harness runs the server `--read-only` with credentials stripped, so a
  result never depends on whose machine it ran on. That is why retrieval
  RESULTS are out of scope here; assertions needing a credential are skipped
  with their reason, never faked.

## PR checklist

- [ ] `DIR2MCP_BINARY=... go test ./conformance/... -v` passes, and the output
      shows the assertions actually ran rather than skipped
- [ ] New assertions fail against a server that lacks the behaviour (check it,
      do not assume it)
- [ ] No new module dependencies
- [ ] `README.md` still describes what the suite really covers
