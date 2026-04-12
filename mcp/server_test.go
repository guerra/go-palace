package mcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	emb := embed.NewFakeEmbedder(palace.DefaultEmbeddingDim)
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

// --- Protocol layer tests ---

func TestInitializeVersionNegotiation(t *testing.T) {
	s := setupTestServer(t)

	t.Run("known_version_echoed", func(t *testing.T) {
		resp := sendRequest(t, s, map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      1,
			"params":  map[string]any{"protocolVersion": "2025-03-26"},
		})
		result := resp["result"].(map[string]any)
		if v := result["protocolVersion"].(string); v != "2025-03-26" {
			t.Errorf("got %q, want 2025-03-26", v)
		}
	})

	t.Run("unknown_version_falls_back_to_latest", func(t *testing.T) {
		resp := sendRequest(t, s, map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      2,
			"params":  map[string]any{"protocolVersion": "9999-12-31"},
		})
		result := resp["result"].(map[string]any)
		if v := result["protocolVersion"].(string); v != SupportedProtocolVersions[0] {
			t.Errorf("got %q, want %q", v, SupportedProtocolVersions[0])
		}
	})

	t.Run("missing_version_uses_oldest", func(t *testing.T) {
		resp := sendRequest(t, s, map[string]any{
			"jsonrpc": "2.0",
			"method":  "initialize",
			"id":      3,
			"params":  map[string]any{},
		})
		result := resp["result"].(map[string]any)
		oldest := SupportedProtocolVersions[len(SupportedProtocolVersions)-1]
		if v := result["protocolVersion"].(string); v != oldest {
			t.Errorf("got %q, want %q", v, oldest)
		}
	})
}

func TestNotificationsInitializedReturnsNoResponse(t *testing.T) {
	s := setupTestServer(t)
	data, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "notifications/initialized",
	})
	var out bytes.Buffer
	if err := s.Serve(bytes.NewReader(append(data, '\n')), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("expected no response for notification, got %q", out.String())
	}
}

// --- seedTestData helper ---

func seedTestData(t *testing.T, s *Server) {
	t.Helper()
	drawers := []struct {
		wing, room, content string
	}{
		{"project", "backend", "The authentication module uses JWT tokens for session management. Tokens expire after 24 hours."},
		{"project", "backend", "Database migrations run via Alembic. We use PostgreSQL for the main store."},
		{"project", "frontend", "The React dashboard uses TanStack Query for data fetching."},
		{"notes", "planning", "Q3 roadmap includes mobile app launch and API v2 redesign."},
	}
	for _, d := range drawers {
		resp := sendRequest(t, s, map[string]any{
			"jsonrpc": "2.0",
			"method":  "tools/call",
			"id":      100,
			"params": map[string]any{
				"name": "mempalace_add_drawer",
				"arguments": map[string]any{
					"wing": d.wing, "room": d.room, "content": d.content,
				},
			},
		})
		if _, ok := resp["error"]; ok {
			t.Fatalf("seed add_drawer error: %v", resp["error"])
		}
	}
}

func callTool(t *testing.T, s *Server, name string, args map[string]any) map[string]any {
	t.Helper()
	params := map[string]any{"name": name}
	if args != nil {
		params["arguments"] = args
	}
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      99,
		"params":  params,
	})
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("expected result for %s, got: %v", name, resp)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("missing content for %s: %v", name, result)
	}
	item, _ := content[0].(map[string]any)
	text, _ := item["text"].(string)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(text), &parsed); err != nil {
		t.Fatalf("unmarshal %s result: %v (raw=%q)", name, err, text)
	}
	return parsed
}

// --- Read tool tests ---

func TestToolStatusEmpty(t *testing.T) {
	s := setupTestServer(t)
	result := callTool(t, s, "mempalace_status", nil)
	total, _ := result["total_drawers"].(float64)
	if total != 0 {
		t.Errorf("total_drawers = %v, want 0", total)
	}
}

func TestToolStatusWithData(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)
	result := callTool(t, s, "mempalace_status", nil)
	total, _ := result["total_drawers"].(float64)
	if total != 4 {
		t.Errorf("total_drawers = %v, want 4", total)
	}
	wings, ok := result["wings"].(map[string]any)
	if !ok {
		t.Fatal("missing wings")
	}
	if _, ok := wings["project"]; !ok {
		t.Error("missing project wing")
	}
	if _, ok := wings["notes"]; !ok {
		t.Error("missing notes wing")
	}
}

func TestToolListWings(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)
	result := callTool(t, s, "mempalace_list_wings", nil)
	wings, ok := result["wings"].(map[string]any)
	if !ok {
		t.Fatal("missing wings")
	}
	if v, _ := wings["project"].(float64); v != 3 {
		t.Errorf("project = %v, want 3", v)
	}
	if v, _ := wings["notes"].(float64); v != 1 {
		t.Errorf("notes = %v, want 1", v)
	}
}

func TestToolListRooms(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)

	t.Run("all_rooms", func(t *testing.T) {
		result := callTool(t, s, "mempalace_list_rooms", nil)
		rooms, ok := result["rooms"].(map[string]any)
		if !ok {
			t.Fatal("missing rooms")
		}
		if _, ok := rooms["backend"]; !ok {
			t.Error("missing backend room")
		}
		if _, ok := rooms["frontend"]; !ok {
			t.Error("missing frontend room")
		}
		if _, ok := rooms["planning"]; !ok {
			t.Error("missing planning room")
		}
	})

	t.Run("filtered_by_wing", func(t *testing.T) {
		result := callTool(t, s, "mempalace_list_rooms", map[string]any{"wing": "project"})
		rooms, ok := result["rooms"].(map[string]any)
		if !ok {
			t.Fatal("missing rooms")
		}
		if _, ok := rooms["backend"]; !ok {
			t.Error("missing backend in project wing")
		}
		if _, ok := rooms["planning"]; ok {
			t.Error("planning should not be in project wing")
		}
	})
}

func TestToolGetTaxonomy(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)
	result := callTool(t, s, "mempalace_get_taxonomy", nil)
	taxonomy, ok := result["taxonomy"].(map[string]any)
	if !ok {
		t.Fatal("missing taxonomy")
	}
	project, _ := taxonomy["project"].(map[string]any)
	if v, _ := project["backend"].(float64); v != 2 {
		t.Errorf("project/backend = %v, want 2", v)
	}
	if v, _ := project["frontend"].(float64); v != 1 {
		t.Errorf("project/frontend = %v, want 1", v)
	}
	notes, _ := taxonomy["notes"].(map[string]any)
	if v, _ := notes["planning"].(float64); v != 1 {
		t.Errorf("notes/planning = %v, want 1", v)
	}
}

// --- Search tool tests ---

func TestToolSearch(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)

	t.Run("basic", func(t *testing.T) {
		result := callTool(t, s, "mempalace_search", map[string]any{"query": "JWT authentication tokens"})
		count, _ := result["count"].(float64)
		if count == 0 {
			t.Error("expected search results")
		}
	})

	t.Run("wing_filter", func(t *testing.T) {
		result := callTool(t, s, "mempalace_search", map[string]any{"query": "planning", "wing": "notes"})
		memories, _ := result["memories"].([]any)
		for _, m := range memories {
			mem, _ := m.(map[string]any)
			if w, _ := mem["wing"].(string); w != "notes" {
				t.Errorf("expected wing=notes, got %q", w)
			}
		}
	})

	t.Run("room_filter", func(t *testing.T) {
		result := callTool(t, s, "mempalace_search", map[string]any{"query": "database", "room": "backend"})
		memories, _ := result["memories"].([]any)
		for _, m := range memories {
			mem, _ := m.(map[string]any)
			if r, _ := mem["room"].(string); r != "backend" {
				t.Errorf("expected room=backend, got %q", r)
			}
		}
	})
}

// --- Write tool tests ---

func TestToolAddDrawer(t *testing.T) {
	s := setupTestServer(t)
	result := callTool(t, s, "mempalace_add_drawer", map[string]any{
		"wing": "test_wing", "room": "test_room",
		"content": "Test content about Python decorators.",
	})
	if success, _ := result["success"].(bool); !success {
		t.Error("add_drawer failed")
	}
	if w, _ := result["wing"].(string); w != "test_wing" {
		t.Errorf("wing = %q, want test_wing", w)
	}
	if r, _ := result["room"].(string); r != "test_room" {
		t.Errorf("room = %q, want test_room", r)
	}
	id, _ := result["drawer_id"].(string)
	if !strings.HasPrefix(id, "drawer_test_wing_test_room_") {
		t.Errorf("drawer_id = %q, unexpected prefix", id)
	}
}

func TestToolDeleteDrawer(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)

	t.Run("existing", func(t *testing.T) {
		// Get a drawer ID by adding one
		add := callTool(t, s, "mempalace_add_drawer", map[string]any{
			"wing": "del_test", "room": "r", "content": "to be deleted",
		})
		id, _ := add["drawer_id"].(string)
		del := callTool(t, s, "mempalace_delete_drawer", map[string]any{"drawer_id": id})
		if success, _ := del["success"].(bool); !success {
			t.Error("delete existing drawer failed")
		}
	})

	t.Run("not_found", func(t *testing.T) {
		del := callTool(t, s, "mempalace_delete_drawer", map[string]any{"drawer_id": "nonexistent_drawer_xyz"})
		if success, _ := del["success"].(bool); success {
			t.Error("delete nonexistent should return success=false")
		}
	})
}

func TestToolCheckDuplicate(t *testing.T) {
	s := setupTestServer(t)
	seedTestData(t, s)

	t.Run("duplicate", func(t *testing.T) {
		result := callTool(t, s, "mempalace_check_duplicate", map[string]any{
			"content":   "The authentication module uses JWT tokens for session management. Tokens expire after 24 hours.",
			"threshold": 0.5,
		})
		if dup, _ := result["is_duplicate"].(bool); !dup {
			t.Error("expected is_duplicate=true for near-identical content")
		}
	})

	t.Run("not_duplicate", func(t *testing.T) {
		result := callTool(t, s, "mempalace_check_duplicate", map[string]any{
			"content":   "Black holes emit Hawking radiation at the event horizon.",
			"threshold": 0.99,
		})
		if dup, _ := result["is_duplicate"].(bool); dup {
			t.Error("expected is_duplicate=false for unrelated content")
		}
	})
}

// --- KG tool tests ---

func seedKGData(t *testing.T, s *Server) {
	t.Helper()
	triples := []struct {
		subject, predicate, object, validFrom, validTo string
	}{
		{"Alice", "parent_of", "Max", "2015-04-01", ""},
		{"Alice", "works_at", "Acme Corp", "2020-01-01", "2024-12-31"},
		{"Alice", "works_at", "NewCo", "2025-01-01", ""},
		{"Max", "does", "swimming", "2025-01-01", ""},
		{"Max", "does", "chess", "2025-06-01", ""},
	}
	for _, tr := range triples {
		args := map[string]any{
			"subject":   tr.subject,
			"predicate": tr.predicate,
			"object":    tr.object,
		}
		if tr.validFrom != "" {
			args["valid_from"] = tr.validFrom
		}
		result := callTool(t, s, "mempalace_kg_add", args)
		if success, _ := result["success"].(bool); !success {
			t.Fatalf("seed kg_add failed: %v", result)
		}
	}
	// Invalidate Acme Corp
	if len(triples) > 1 {
		callTool(t, s, "mempalace_kg_invalidate", map[string]any{
			"subject": "Alice", "predicate": "works_at", "object": "Acme Corp", "ended": "2024-12-31",
		})
	}
}

func TestToolKGAdd(t *testing.T) {
	s := setupTestServer(t)
	result := callTool(t, s, "mempalace_kg_add", map[string]any{
		"subject": "Alice", "predicate": "likes", "object": "coffee", "valid_from": "2025-01-01",
	})
	if success, _ := result["success"].(bool); !success {
		t.Error("kg_add failed")
	}
}

func TestToolKGQuery(t *testing.T) {
	s := setupTestServer(t)
	seedKGData(t, s)
	result := callTool(t, s, "mempalace_kg_query", map[string]any{"entity": "Max"})
	count, _ := result["count"].(float64)
	if count == 0 {
		t.Error("expected count > 0")
	}
}

func TestToolKGInvalidate(t *testing.T) {
	s := setupTestServer(t)
	seedKGData(t, s)
	result := callTool(t, s, "mempalace_kg_invalidate", map[string]any{
		"subject": "Max", "predicate": "does", "object": "chess", "ended": "2026-03-01",
	})
	if success, _ := result["success"].(bool); !success {
		t.Error("kg_invalidate failed")
	}
}

func TestToolKGTimeline(t *testing.T) {
	s := setupTestServer(t)
	seedKGData(t, s)
	result := callTool(t, s, "mempalace_kg_timeline", map[string]any{"entity": "Alice"})
	count, _ := result["count"].(float64)
	if count == 0 {
		t.Error("expected timeline count > 0")
	}
}

func TestToolKGStats(t *testing.T) {
	s := setupTestServer(t)
	seedKGData(t, s)
	result := callTool(t, s, "mempalace_kg_stats", nil)
	entities, _ := result["entities"].(float64)
	if entities < 4 {
		t.Errorf("expected entities >= 4, got %v", entities)
	}
}

// --- Diary tool tests ---

func TestToolDiaryWriteAndRead(t *testing.T) {
	s := setupTestServer(t)
	w := callTool(t, s, "mempalace_diary_write", map[string]any{
		"agent_name": "TestAgent",
		"entry":      "Today we discussed authentication patterns.",
		"topic":      "architecture",
	})
	if success, _ := w["success"].(bool); !success {
		t.Fatal("diary_write failed")
	}
	if agent, _ := w["agent"].(string); agent != "TestAgent" {
		t.Errorf("agent = %q, want TestAgent", agent)
	}

	// Small delay to ensure filed_at differs
	time.Sleep(10 * time.Millisecond)

	r := callTool(t, s, "mempalace_diary_read", map[string]any{"agent_name": "TestAgent"})
	total, _ := r["total"].(float64)
	if total != 1 {
		t.Errorf("total = %v, want 1", total)
	}
	entries, _ := r["entries"].([]any)
	if len(entries) == 0 {
		t.Fatal("no entries returned")
	}
	entry, _ := entries[0].(map[string]any)
	if topic, _ := entry["topic"].(string); topic != "architecture" {
		t.Errorf("topic = %q, want architecture", topic)
	}
	if content, _ := entry["content"].(string); !strings.Contains(content, "authentication") {
		t.Error("entry content missing 'authentication'")
	}
}

func TestToolDiaryReadEmpty(t *testing.T) {
	s := setupTestServer(t)
	r := callTool(t, s, "mempalace_diary_read", map[string]any{"agent_name": "Nobody"})
	entries, _ := r["entries"].([]any)
	if len(entries) != 0 {
		t.Errorf("expected empty entries, got %d", len(entries))
	}
}

// --- Null/edge tests ---

func TestNullArgumentsDoesNotHang(t *testing.T) {
	s := setupTestServer(t)
	resp := sendRequest(t, s, map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      10,
		"params":  map[string]any{"name": "mempalace_status", "arguments": nil},
	})
	if _, ok := resp["error"]; ok {
		t.Fatalf("unexpected error: %v", resp["error"])
	}
}
