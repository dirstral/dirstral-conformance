# dirstral-conformance

Language-agnostic black-box conformance harness for dirstral-spec compliant MCP servers.

## Scope

This repo validates externally observable behavior only:
- lifecycle and session behavior
- tool schema conformance
- canonical error behavior
- x402 mode behavior

## Usage

```bash
DIR2MCP_BINARY=/path/to/server-binary go test -v ./conformance/...
```

The harness must never import implementation internals.
