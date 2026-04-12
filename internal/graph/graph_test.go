package graph_test

import (
	"path/filepath"
	"testing"
	"time"

	"go-palace/internal/embed"
	"go-palace/internal/graph"
	"go-palace/internal/palace"
)

func setupGraphPalace(t *testing.T) *palace.Palace {
	t.Helper()
	p, err := palace.Open(filepath.Join(t.TempDir(), "p.db"),
		embed.NewFakeEmbedder(palace.EmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	drawers := []palace.Drawer{
		{ID: "d1", Document: "about golang", Wing: "wing_code", Room: "golang", SourceFile: "a.go", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 1, 10, 0, 0, 0, 0, time.UTC), Metadata: map[string]any{"hall": "tutorials"}},
		{ID: "d2", Document: "more golang", Wing: "wing_code", Room: "golang", SourceFile: "b.go", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 1, 11, 0, 0, 0, 0, time.UTC), Metadata: map[string]any{"hall": "tutorials"}},
		{ID: "d3", Document: "golang in my project", Wing: "wing_myproject", Room: "golang", SourceFile: "c.go", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 2, 1, 0, 0, 0, 0, time.UTC), Metadata: map[string]any{"hall": "setup"}},
		{ID: "d4", Document: "python basics", Wing: "wing_code", Room: "python", SourceFile: "d.py", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 3, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "d5", Document: "rust intro", Wing: "wing_code", Room: "rust", SourceFile: "e.rs", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 4, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "d6", Document: "rust in my project", Wing: "wing_myproject", Room: "rust", SourceFile: "f.rs", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Date(2025, 4, 2, 0, 0, 0, 0, time.UTC)},
		// general room should be skipped
		{ID: "d7", Document: "general stuff", Wing: "wing_code", Room: "general", SourceFile: "g.txt", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now()},
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return p
}

func TestBuildGraph(t *testing.T) {
	p := setupGraphPalace(t)
	nodes, edges, err := graph.BuildGraph(p)
	if err != nil {
		t.Fatalf("BuildGraph: %v", err)
	}
	// Should have golang, python, rust (not general)
	if len(nodes) != 3 {
		t.Errorf("nodes: got %d, want 3", len(nodes))
	}
	if _, ok := nodes["general"]; ok {
		t.Error("general should be excluded")
	}
	// golang has 2 wings → tunnel
	goNode := nodes["golang"]
	if len(goNode.Wings) != 2 {
		t.Errorf("golang wings: got %d, want 2", len(goNode.Wings))
	}
	if goNode.Count != 3 {
		t.Errorf("golang count: got %d, want 3", goNode.Count)
	}
	// Edges: golang has 2 wings with 2 halls → 2 edges; rust has 2 wings with 0 halls → 0 edges
	if len(edges) != 2 {
		t.Errorf("edges: got %d, want 2", len(edges))
	}
}

func TestTraverse(t *testing.T) {
	p := setupGraphPalace(t)
	results, err := graph.Traverse(p, "golang", 2)
	if err != nil {
		t.Fatalf("Traverse: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].Room != "golang" || results[0].Hop != 0 {
		t.Errorf("first result should be start room at hop 0, got %+v", results[0])
	}
	// Should find rust through shared wing_myproject and wing_code,
	// and python through shared wing_code
	found := map[string]bool{}
	for _, r := range results {
		found[r.Room] = true
	}
	if !found["rust"] {
		t.Error("expected to find rust via shared wings")
	}
	if !found["python"] {
		t.Error("expected to find python via shared wing_code")
	}
	if len(results) > 50 {
		t.Error("results should be capped at 50")
	}
}

func TestTraverseNotFound(t *testing.T) {
	p := setupGraphPalace(t)
	_, err := graph.Traverse(p, "nonexistent-room", 2)
	if err == nil {
		t.Fatal("expected error for nonexistent room")
	}
	te, ok := err.(*graph.TraverseError)
	if !ok {
		t.Fatalf("expected TraverseError, got %T", err)
	}
	if len(te.Suggestions) == 0 {
		// May or may not have suggestions depending on fuzzy match
		t.Log("no suggestions (expected for completely unrelated name)")
	}
}

func TestFindTunnels(t *testing.T) {
	p := setupGraphPalace(t)
	tunnels, err := graph.FindTunnels(p, "", "")
	if err != nil {
		t.Fatalf("FindTunnels: %v", err)
	}
	// golang and rust both have 2+ wings
	if len(tunnels) < 2 {
		t.Errorf("tunnels: got %d, want >=2", len(tunnels))
	}
	// Filter by wing
	tunnels, err = graph.FindTunnels(p, "wing_myproject", "")
	if err != nil {
		t.Fatalf("FindTunnels filtered: %v", err)
	}
	for _, tunnel := range tunnels {
		hasWing := false
		for _, w := range tunnel.Wings {
			if w == "wing_myproject" {
				hasWing = true
			}
		}
		if !hasWing {
			t.Errorf("tunnel %s missing wing_myproject", tunnel.Room)
		}
	}
}

func TestStats(t *testing.T) {
	p := setupGraphPalace(t)
	stats, err := graph.Stats(p)
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if stats.TotalRooms != 3 {
		t.Errorf("TotalRooms: got %d, want 3", stats.TotalRooms)
	}
	if stats.TunnelRooms < 2 {
		t.Errorf("TunnelRooms: got %d, want >=2", stats.TunnelRooms)
	}
	if stats.TotalEdges != 2 {
		t.Errorf("TotalEdges: got %d, want 2", stats.TotalEdges)
	}
	if stats.RoomsPerWing["wing_code"] != 3 {
		t.Errorf("RoomsPerWing[wing_code]: got %d, want 3", stats.RoomsPerWing["wing_code"])
	}
}

func TestFuzzyMatch(t *testing.T) {
	p := setupGraphPalace(t)
	_, err := graph.Traverse(p, "go", 2)
	if err == nil {
		t.Fatal("expected error for partial room name")
	}
	te, ok := err.(*graph.TraverseError)
	if !ok {
		t.Fatalf("expected TraverseError, got %T", err)
	}
	// "go" should partially match "golang"
	found := false
	for _, s := range te.Suggestions {
		if s == "golang" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'golang' in suggestions, got %v", te.Suggestions)
	}
}
