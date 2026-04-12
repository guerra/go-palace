package mcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"go-palace/internal/config"
	"go-palace/internal/embed"
	"go-palace/internal/kg"
	"go-palace/internal/palace"
)

func setupTestServer(t *testing.T) *Server {
	t.Helper()
	dir := t.TempDir()
	palacePath := filepath.Join(dir, "palace.db")
	kgPath := filepath.Join(dir, "kg.db")

	emb := embed.NewFakeEmbedder(384)
	p, err := palace.Open(palacePath, emb)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })

	kgDB, err := kg.Open(kgPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = kgDB.Close() })

	cfg := &config.Config{}
	return NewServer(palacePath, p, kgDB, cfg)
}

func sendRequest(t *testing.T, s *Server, req map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := s.Serve(bytes.NewReader(append(data, '\n')), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	var resp map[string]any
	if err := json.Unmarshal(out.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v (raw=%q)", err, out.String())
	}
	return resp
}

func TestInitialize(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "initialize",
		"id":      1,
		"params":  map[string]any{"protocolVersion": "2024-11-05"},
	})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resp)
	}
	if v, _ := result["protocolVersion"].(string); v != "2024-11-05" {
		t.Errorf("protocolVersion = %q, want 2024-11-05", v)
	}
	serverInfo, _ := result["serverInfo"].(map[string]any)
	if name, _ := serverInfo["name"].(string); name != "mempalace" {
		t.Errorf("serverInfo.name = %q", name)
	}
}

func TestToolsList(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/list",
		"id":      2,
	})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("missing tools: %v", result)
	}
	if len(tools) != 19 {
		t.Errorf("expected 19 tools, got %d", len(tools))
	}

	// Verify some key tool names exist.
	names := map[string]bool{}
	for _, tool := range tools {
		m, _ := tool.(map[string]any)
		name, _ := m["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"mempalace_status", "mempalace_search", "mempalace_add_drawer", "mempalace_kg_query"} {
		if !names[want] {
			t.Errorf("missing tool %q", want)
		}
	}
}

func TestToolsCallStatus(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      3,
		"params":  map[string]any{"name": "mempalace_status"},
	})

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("missing result: %v", resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("missing content: %v", result)
	}
	item, _ := content[0].(map[string]any)
	text, _ := item["text"].(string)
	if !strings.Contains(text, "total_drawers") {
		t.Errorf("status response missing total_drawers: %s", text)
	}
}

func TestToolsCallUnknownTool(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      4,
		"params":  map[string]any{"name": "nonexistent_tool"},
	})

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got: %v", resp)
	}
	code, _ := errObj["code"].(float64)
	if code != -32601 {
		t.Errorf("error code = %v, want -32601", code)
	}
}

func TestUnknownMethod(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "bogus/method",
		"id":      5,
	})

	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error response, got: %v", resp)
	}
	code, _ := errObj["code"].(float64)
	if code != -32601 {
		t.Errorf("error code = %v, want -32601", code)
	}
}

func TestTypeCoercion(t *testing.T) {
	s := setupTestServer(t)
	// Pass limit as string — should be coerced to int.
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      6,
		"params": map[string]any{
			"name":      "mempalace_search",
			"arguments": map[string]any{"query": "test", "limit": "3"},
		},
	})

	// Should not error.
	if _, ok := resp["error"]; ok {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}

func TestAddDrawerIdempotent(t *testing.T) {
	s := setupTestServer(t)

	args := map[string]any{
		"wing":    "test_wing",
		"room":    "test_room",
		"content": "test content for idempotency check",
	}

	// First add.
	resp1 := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      7,
		"params":  map[string]any{"name": "mempalace_add_drawer", "arguments": args},
	})
	result1, _ := resp1["result"].(map[string]any)
	content1, _ := result1["content"].([]any)
	item1, _ := content1[0].(map[string]any)
	text1, _ := item1["text"].(string)

	var r1 map[string]any
	_ = json.Unmarshal([]byte(text1), &r1)
	if success, _ := r1["success"].(bool); !success {
		t.Fatalf("first add failed: %s", text1)
	}

	// Second add — same content.
	resp2 := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      8,
		"params":  map[string]any{"name": "mempalace_add_drawer", "arguments": args},
	})
	result2, _ := resp2["result"].(map[string]any)
	content2, _ := result2["content"].([]any)
	item2, _ := content2[0].(map[string]any)
	text2, _ := item2["text"].(string)

	var r2 map[string]any
	_ = json.Unmarshal([]byte(text2), &r2)
	reason, _ := r2["reason"].(string)
	if reason != "already_exists" {
		t.Errorf("second add should return already_exists, got: %s", text2)
	}
}
