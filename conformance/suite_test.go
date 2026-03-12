// Package conformance is the black-box conformance harness for dirstral-spec compliant servers.
// It drives a server binary over stdio or HTTP and asserts externally-observable behavior.
//
// Usage:
//
//	DIR2MCP_BINARY=/path/to/dir2mcp go test ./conformance/... -v
//
// The suite is implementation-agnostic: point DIR2MCP_BINARY at any spec-compliant binary.
package conformance_test

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// TODO: start server binary before suite, tear down after.
	// Controlled by DIR2MCP_BINARY env var.
	// Skipped if binary not provided.
	if os.Getenv("DIR2MCP_BINARY") == "" {
		// Allow running without a binary for CI stub validation.
	}
	os.Exit(m.Run())
}

// TestLifecycle_Initialize verifies that the server responds correctly to the MCP initialize handshake.
func TestLifecycle_Initialize(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestLifecycle_ToolsList verifies that tools/list returns the expected tool surface.
func TestLifecycle_ToolsList(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestLifecycle_SessionRecovery verifies that SESSION_NOT_FOUND triggers proper re-initialization.
func TestLifecycle_SessionRecovery(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestTools_Search verifies search tool schema and response shape.
func TestTools_Search(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestTools_Ask verifies ask tool schema and response shape.
func TestTools_Ask(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestTools_OpenFile verifies open_file tool schema and response shape.
func TestTools_OpenFile(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestTools_ListFiles verifies list_files tool schema and response shape.
func TestTools_ListFiles(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestErrors_Canonical verifies that canonical error codes are returned correctly.
func TestErrors_Canonical(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestX402_ModeOff verifies that x402=off passes requests through with no payment headers.
func TestX402_ModeOff(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestX402_ModeOn verifies x402=on behavior (fail-open on facilitator unavailable).
func TestX402_ModeOn(t *testing.T) {
	t.Skip("stub: implement in #105")
}

// TestX402_ModeRequired verifies x402=required behavior (fail-closed on facilitator unavailable).
func TestX402_ModeRequired(t *testing.T) {
	t.Skip("stub: implement in #105")
}
