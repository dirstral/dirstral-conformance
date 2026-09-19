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

const protocolVersion = "2025-11-25"

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
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
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
	req.Header.Set("MCP-Protocol-Version", protocolVersion)
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

	text := unwrapSSE(string(payload))
	var envelope struct {
		Result map[string]interface{} `json:"result"`
		Error  *rpcError              `json:"error"`
	}
	if err := json.Unmarshal([]byte(text), &envelope); err != nil {
		return nil, text, fmt.Errorf("%s: status %d, undecodable body: %w", method, resp.StatusCode, err)
	}
	if envelope.Error != nil {
		return nil, text, envelope.Error
	}
	return envelope.Result, text, nil
}

// unwrapSSE extracts the JSON payload from a text/event-stream response.
// A server answering application/json is passed through unchanged.
func unwrapSSE(body string) string {
	if !strings.Contains(body, "data:") {
		return body
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "data:"); ok {
			out = append(out, strings.TrimSpace(rest))
		}
	}
	if len(out) == 0 {
		return body
	}
	return strings.Join(out, "")
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
	out := toolResult{Raw: raw}
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
