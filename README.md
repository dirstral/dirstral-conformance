# dirstral-conformance

Language-agnostic black-box conformance harness for dirstral-spec compliant MCP servers.

## Why this exists outside the reference implementation

`dirstral-spec` mandates it. From `spec/versioning.md`, the breaking-change process:

> 5. `dirstral-conformance` must add a new test suite for the new behavior

dir2mcp has its own in-tree conformance suite, and it is good, but it is Go-package-scoped and can only ever validate dir2mcp. The compatibility matrix names more than one implementation. A harness that drives a *binary* is the only thing that can hold all of them to the same contract.

## Scope

Externally observable behaviour only. The harness never imports server internals.

- lifecycle and session behaviour
- the advertised tool surface and its schemas
- canonical error behaviour
- answer provenance (SPEC 9.4.5)
- x402 mode behaviour

## Usage

```bash
DIR2MCP_BINARY=/path/to/server-binary go test ./conformance/... -v
```

The binary must accept the documented startup flags: `--config`, `up --dir --state-dir --foreground --json --non-interactive --read-only --auth --listen`, and emit NDJSON on stdout carrying the listening URL. That command line IS the portability contract; an implementation that accepts it can be held to this suite.

Without `DIR2MCP_BINARY` the suite compiles and skips. It says so loudly rather than reporting a quiet green: see `TestHarness_RefusesToPassVacuously`.

## What a credential-free run covers

The harness starts the server `--read-only` and with provider credentials stripped from its environment, because a conformance result must not depend on whose machine it runs on. That covers the protocol surface: handshake, sessions, tool schemas, the error taxonomy.

It does **not** cover retrieval RESULTS, which need an indexed corpus and therefore an embedding credential. Those assertions are marked and skipped rather than written to pass vacuously.
