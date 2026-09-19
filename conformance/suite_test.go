// Package conformance_test is the black-box conformance harness for
// dirstral-spec compliant servers.
//
// Usage:
//
//	DIR2MCP_BINARY=/path/to/server go test ./conformance/... -v
//
// It launches the binary, speaks MCP to it over streamable HTTP, and asserts
// only externally-observable behaviour. It never imports server internals, so
// any implementation that accepts the documented flags can be held to the same
// contract. That is the whole reason this lives outside the reference
// implementation: dirstral-spec's change process (spec/versioning.md, step 5)
// requires a conformance suite for new behaviour, and a suite inside one
// server can only ever validate that server.
//
// # What a credential-free run does and does not cover
//
// The harness starts the server with --read-only, because ingestion requires
// an embedding provider and the contracts below do not. So it covers the
// protocol surface: handshake, session behaviour, the advertised tool surface
// and its schemas, and the canonical error taxonomy. It does NOT cover
// retrieval RESULTS, which need an indexed corpus and therefore a credential.
// Those are marked and skipped rather than asserted vacuously; see
// retrieval_test.go.
package conformance_test

import (
	"strings"
	"testing"
)

// corpus is the fixture every test serves. Small and boring on purpose: the
// assertions here are about the protocol, not about retrieval quality.
var corpus = map[string]string{
	"README.md":         "# Fixture\n\nA short document about umbrellas.\n",
	"notes/second.md":   "# Second\n\nA different document about bicycles.\n",
	".hidden/secret.md": "# Hidden\n\nShould not be listed by default.\n",
}

// TestHarness_RefusesToPassVacuously is the guard on everything else.
//
// Every test below skips when DIR2MCP_BINARY is unset, and a skipped suite
// reports the same green as a passing one. Historically this repo WAS that
// green: eleven tests, all t.Skip, for six months. So one test states the
// condition out loud, and a run that asserted nothing says so.
func TestHarness_RefusesToPassVacuously(t *testing.T) {
	if binaryPath() == "" {
		t.Log("DIR2MCP_BINARY is unset: this run compiled the suite and asserted NOTHING about any server.")
		t.Skip("no server under test")
	}
	t.Logf("server under test: %s", binaryPath())
}

// TestLifecycle_Initialize: the handshake returns a protocol version, server
// info, and a session id a subsequent call can use.
func TestLifecycle_Initialize(t *testing.T) {
	srv := requireServer(t, corpus)
	c := dial(t, srv)
	if c.sessionID == "" {
		t.Fatal("no session id after initialize")
	}
	// Proves the id is usable, not merely present.
	if _, _, err := c.call(ctx(t), "tools/list", map[string]interface{}{}); err != nil {
		t.Fatalf("session id from initialize was not accepted: %v", err)
	}
}

// TestLifecycle_ToolsList: the advertised surface carries the spec's tool
// names, and every tool declares an input schema.
func TestLifecycle_ToolsList(t *testing.T) {
	srv := requireServer(t, corpus)
	tools := dial(t, srv).tools(t)

	// The core surface. Deliberately not the full nine: a server MAY omit
	// optional tools, and a conformance suite that demanded all of them would
	// fail a compliant subset implementation.
	for _, name := range []string{
		"dir2mcp_search", "dir2mcp_ask", "dir2mcp_open_file", "dir2mcp_list_files",
	} {
		tool, ok := tools[name]
		if !ok {
			var listed []string
			for have := range tools {
				listed = append(listed, have)
			}
			t.Fatalf("tools/list advertises no %s; it advertises %v", name, listed)
		}
		if _, ok := tool["inputSchema"].(map[string]interface{}); !ok {
			t.Fatalf("%s declares no inputSchema", name)
		}
	}
}

// TestLifecycle_SessionRecovery: an unknown session is rejected rather than
// silently served, and re-initializing recovers.
func TestLifecycle_SessionRecovery(t *testing.T) {
	srv := requireServer(t, corpus)
	c := dial(t, srv)

	stale := &client{url: srv.URL, sessionID: "00000000-0000-4000-8000-000000000000"}
	_, raw, err := stale.call(ctx(t), "tools/list", map[string]interface{}{})
	if err == nil {
		t.Fatalf("an unknown session id was served: %s", raw)
	}
	// The code matters less than the refusal; the taxonomy is asserted in
	// TestErrors_Canonical. What must not happen is being handed results.

	// The original session still works, so the refusal was about the id.
	if _, _, err := c.call(ctx(t), "tools/list", map[string]interface{}{}); err != nil {
		t.Fatalf("the live session broke: %v", err)
	}
	// And a fresh handshake recovers.
	if recovered := dial(t, srv); recovered.sessionID == c.sessionID {
		t.Fatal("re-initialize reused the previous session id")
	}
}

// TestErrors_Canonical: a malformed call is refused with a structured error,
// not a panic, a 500, or an empty success.
func TestErrors_Canonical(t *testing.T) {
	srv := requireServer(t, corpus)
	c := dial(t, srv)

	t.Run("unknown tool", func(t *testing.T) {
		_, raw, err := c.call(ctx(t), "tools/call", map[string]interface{}{
			"name":      "dir2mcp_does_not_exist",
			"arguments": map[string]interface{}{},
		})
		if err == nil {
			t.Fatalf("an unknown tool was accepted: %s", raw)
		}
	})

	t.Run("missing required field", func(t *testing.T) {
		// dir2mcp_ask requires `question`.
		res := c.callTool(t, "dir2mcp_ask", map[string]interface{}{})
		if !res.IsError {
			t.Fatalf("ask with no question succeeded: %s", res.Raw)
		}
		if !mentionsAnyCode(res, "MISSING_FIELD", "INVALID_FIELD") {
			t.Fatalf("ask with no question gave no canonical code: %s", res.Raw)
		}
	})

	t.Run("out of range", func(t *testing.T) {
		// `k` is bounded 1..50 by the published schema.
		res := c.callTool(t, "dir2mcp_search", map[string]interface{}{"query": "x", "k": 9999})
		if !res.IsError {
			t.Fatalf("search with k=9999 succeeded: %s", res.Raw)
		}
		if !mentionsAnyCode(res, "INVALID_RANGE", "INVALID_FIELD") {
			t.Fatalf("search with k=9999 gave no canonical code: %s", res.Raw)
		}
	})
}

// mentionsAnyCode reports whether a tool error names one of the canonical
// codes. Substring matching on the whole reply, because the code may travel in
// the error `data` envelope or in the text block, and the spec's taxonomy is
// what matters rather than the carrier.
func mentionsAnyCode(res toolResult, codes ...string) bool {
	haystack := res.Raw + "\n" + res.Text
	for _, code := range codes {
		if strings.Contains(haystack, code) {
			return true
		}
	}
	return false
}
