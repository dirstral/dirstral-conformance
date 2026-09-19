package conformance_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

// A minimal MCP client over streamable HTTP. Stdlib only, because the module
// declares no dependencies and a conformance harness that needed an SDK would
// be testing the SDK's idea of the protocol rather than the wire contract.

// protocolVersion is what the harness OFFERS. supportedProtocols is what it
// will accept back: a server may negotiate down, and a conformance client has
// to notice rather than keep talking at its own version.
const protocolVersion = "2025-11-25"

var supportedProtocols = []string{"2025-11-25", "2025-06-18", "2025-03-26"}

func supportedProtocol(version string) bool {
	for _, candidate := range supportedProtocols {
		if candidate == version {
			return true
		}
	}
	return false
}

// protocolHeader is the version every request after initialize carries.
func (c *client) protocolHeader() string {
	if c.negotiated != "" {
		return c.negotiated
	}
	return protocolVersion
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message) }

type client struct {
	url       string
	sessionID string
	next      int
	// lastStatus is the HTTP status of the most recent reply. The spec makes
	// the transport status part of the contract (a TOOL error is HTTP 200
	// with isError true, not a 5xx), so callers can assert it.
	lastStatus int
	// negotiated is the protocol version the server chose at initialize.
	// Every later request carries it, rather than the one we asked for.
	negotiated string
}

// dial performs the initialize handshake and returns a ready client.
func dial(t *testing.T, srv *server) *client {
	t.Helper()
	c := &client{url: srv.URL}
	res, raw, err := c.call(ctx(t), "initialize", map[string]interface{}{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "dirstral-conformance", "version": "0"},
	})
	if err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if c.sessionID == "" {
		t.Fatalf("initialize returned no MCP-Session-Id header; body=%s", raw)
	}
	if _, ok := res["serverInfo"]; !ok {
		t.Fatalf("initialize result declares no serverInfo: %s", raw)
	}
	// protocolVersion is REQUIRED on InitializeResult, and the server may pick
	// a different one from the one asked for. Ignoring it left the client
	// asserting against a version the server never agreed to, and sending that
	// version in every later header.
	negotiated, ok := res["protocolVersion"].(string)
	if !ok || negotiated == "" {
		t.Fatalf("initialize result declares no protocolVersion: %s", raw)
	}
	if !supportedProtocol(negotiated) {
		t.Fatalf("server negotiated protocol %q, which this harness does not speak (it offers %v)",
			negotiated, supportedProtocols)
	}
	c.negotiated = negotiated
	// MCP requires the client to confirm the handshake before issuing any
	// other request. Skipping it is refused with SESSION_NOT_INITIALIZED,
	// which the harness found on its first run against a real server.
	if err := c.notify(ctx(t), "notifications/initialized", map[string]interface{}{}); err != nil {
		t.Fatalf("notifications/initialized: %v", err)
	}
	return c
}

// notify sends a JSON-RPC notification: no id, and no response to decode.
func (c *client) notify(ctx context.Context, method string, params interface{}) error {
	body := map[string]interface{}{"jsonrpc": "2.0", "method": method}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode %s: %w", method, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(encoded))
	if err != nil {
		return fmt.Errorf("build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.protocolHeader())
	if c.sessionID != "" {
		req.Header.Set("MCP-Session-Id", c.sessionID)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: status %d: %s", method, resp.StatusCode, string(payload))
	}
	return nil
}

// call sends one JSON-RPC request and returns the decoded `result`.
//
// A JSON-RPC error is returned as *rpcError, NOT as a test failure: several
// contracts here are about the error, so the caller decides.
func (c *client) call(ctx context.Context, method string, params interface{}) (map[string]interface{}, string, error) {
	c.next++
	body := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      c.next,
		"method":  method,
	}
	if params != nil {
		body["params"] = params
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return nil, "", fmt.Errorf("encode %s: %w", method, err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(encoded))
	if err != nil {
		return nil, "", fmt.Errorf("build %s: %w", method, err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Both, because a streamable-HTTP server may answer either way and a
	// conformance client must not force one.
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("MCP-Protocol-Version", c.protocolHeader())
	if c.sessionID != "" {
		req.Header.Set("MCP-Session-Id", c.sessionID)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", method, err)
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", fmt.Errorf("read %s: %w", method, err)
	}
	if id := resp.Header.Get("MCP-Session-Id"); id != "" {
		c.sessionID = id
	}

	text := string(payload)
	envelope, err := selectResponse(text, c.next)
	if err != nil {
		return nil, text, fmt.Errorf("%s: status %d: %w", method, resp.StatusCode, err)
	}
	// The transport status is part of the contract, so it travels with the
	// result instead of being consulted only when decoding failed. A 500
	// carrying a JSON-RPC error would otherwise satisfy any caller that
	// checks `err != nil` and nothing else.
	c.lastStatus = resp.StatusCode
	if envelope.Error != nil {
		return nil, text, envelope.Error
	}
	return envelope.Result, text, nil
}

type rpcEnvelope struct {
	ID     interface{}            `json:"id"`
	Result map[string]interface{} `json:"result"`
	Error  *rpcError              `json:"error"`
}

// selectResponse finds the reply to request id among everything the server
// sent, and ignores the rest.
//
// Streamable HTTP allows a server to interleave its own requests and
// notifications ahead of the response. Concatenating every `data:` field
// produced one invalid JSON value as soon as that happened, so each event is
// decoded on its own and correlated by id.
func selectResponse(body string, id int) (*rpcEnvelope, error) {
	messages := sseMessages(body)
	if len(messages) == 0 {
		return nil, fmt.Errorf("no JSON-RPC message in the body: %s", truncate(body))
	}
	var decodeErr error
	for _, message := range messages {
		var envelope rpcEnvelope
		if err := json.Unmarshal([]byte(message), &envelope); err != nil {
			decodeErr = err
			continue
		}
		// A notification carries no id and is never the reply.
		if envelope.ID == nil {
			continue
		}
		if number, ok := envelope.ID.(float64); ok && int(number) == id {
			return &envelope, nil
		}
	}
	if decodeErr != nil {
		return nil, fmt.Errorf("undecodable message: %w", decodeErr)
	}
	return nil, fmt.Errorf("no reply to request id %d: %s", id, truncate(body))
}

// sseMessages splits a body into candidate JSON-RPC messages: one per SSE
// event, or the whole body when the server answered application/json.
func sseMessages(body string) []string {
	if !strings.Contains(body, "data:") {
		if strings.TrimSpace(body) == "" {
			return nil
		}
		return []string{body}
	}
	var messages []string
	var current []string
	flush := func() {
		if len(current) > 0 {
			messages = append(messages, strings.Join(current, "\n"))
			current = nil
		}
	}
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimRight(line, "\r")
		if strings.TrimSpace(trimmed) == "" {
			// A blank line terminates one SSE event.
			flush()
			continue
		}
		if rest, ok := strings.CutPrefix(trimmed, "data:"); ok {
			current = append(current, strings.TrimSpace(rest))
		}
	}
	flush()
	return messages
}

func truncate(s string) string {
	const limit = 400
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "..."
}

// tools returns tools/list keyed by tool name.
func (c *client) tools(t *testing.T) map[string]map[string]interface{} {
	t.Helper()
	res, raw, err := c.call(ctx(t), "tools/list", map[string]interface{}{})
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	listed, ok := res["tools"].([]interface{})
	if !ok {
		t.Fatalf("tools/list result has no tools array: %s", raw)
	}
	out := make(map[string]map[string]interface{}, len(listed))
	for _, entry := range listed {
		tool, ok := entry.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := tool["name"].(string)
		if name != "" {
			out[name] = tool
		}
	}
	if len(out) == 0 {
		t.Fatalf("tools/list advertised no named tools: %s", raw)
	}
	return out
}

// toolResult is the decoded shape of a tools/call reply.
type toolResult struct {
	Status     int
	IsError    bool
	Structured map[string]interface{}
	Text       string
	Raw        string
}

// callTool invokes a tool. A tool-execution error arrives as isError=true with
// HTTP 200, so it is returned as data rather than as a Go error.
func (c *client) callTool(t *testing.T, name string, args map[string]interface{}) toolResult {
	t.Helper()
	res, raw, err := c.call(ctx(t), "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		t.Fatalf("tools/call %s: %v", name, err)
	}
	// SPEC: a TOOL-execution error is HTTP 200 with isError true, not a 5xx.
	// Without this, a server that 500s on a bad argument satisfies every
	// isError assertion below and the harness calls it conformant.
	if c.lastStatus != http.StatusOK {
		t.Fatalf("tools/call %s returned HTTP %d; a tool error is 200 with isError true: %s",
			name, c.lastStatus, raw)
	}
	out := toolResult{Raw: raw, Status: c.lastStatus}
	if flag, ok := res["isError"].(bool); ok {
		out.IsError = flag
	}
	if structured, ok := res["structuredContent"].(map[string]interface{}); ok {
		out.Structured = structured
	}
	if content, ok := res["content"].([]interface{}); ok {
		var parts []string
		for _, item := range content {
			block, ok := item.(map[string]interface{})
			if !ok {
				continue
			}
			if text, ok := block["text"].(string); ok {
				parts = append(parts, text)
			}
		}
		out.Text = strings.Join(parts, "\n")
	}
	return out
}
