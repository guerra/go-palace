package graph_test

import (
	"fmt"
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
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
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

func TestBuildGraph_EmptyPalace(t *testing.T) {
	p, err := palace.Open(filepath.Join(t.TempDir(), "empty.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	nodes, edges, err := graph.BuildGraph(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 0 {
		t.Errorf("empty palace should have 0 nodes, got %d", len(nodes))
	}
	if len(edges) != 0 {
		t.Errorf("empty palace should have 0 edges, got %d", len(edges))
	}
}

func TestBuildGraph_MissingWingExcluded(t *testing.T) {
	p, err := palace.Open(filepath.Join(t.TempDir(), "p.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	// Drawer with empty wing.
	if err := p.UpsertBatch([]palace.Drawer{{
		ID: "orphan", Document: "orphan", Wing: "", Room: "orphan",
		SourceFile: "x", AddedBy: "test", FiledAt: time.Now(),
	}}); err != nil {
		t.Fatal(err)
	}
	nodes, _, err := graph.BuildGraph(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := nodes["orphan"]; ok {
		t.Error("drawer with empty wing should be excluded")
	}
}

func TestBuildGraph_DatesCappedAtFive(t *testing.T) {
	p, err := palace.Open(filepath.Join(t.TempDir(), "p.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	var drawers []palace.Drawer
	for i := 1; i <= 9; i++ {
		drawers = append(drawers, palace.Drawer{
			ID: fmt.Sprintf("d%d", i), Document: "busy content", Wing: "w", Room: "busy",
			SourceFile: fmt.Sprintf("f%d", i), ChunkIndex: 0, AddedBy: "test",
			FiledAt: time.Date(2026, 1, i, 0, 0, 0, 0, time.UTC),
		})
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatal(err)
	}

	nodes, _, err := graph.BuildGraph(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes["busy"].Dates) > 5 {
		t.Errorf("dates should be capped at 5, got %d", len(nodes["busy"].Dates))
	}
}

func TestBuildGraph_MultiWingCreatesEdges(t *testing.T) {
	p := setupGraphPalace(t)
	nodes, edges, err := graph.BuildGraph(p)
	if err != nil {
		t.Fatal(err)
	}
	// golang has 2 wings + halls -> edges
	goNode, ok := nodes["golang"]
	if !ok {
		t.Fatal("golang node not found")
	}
	if len(goNode.Wings) < 2 {
		t.Errorf("golang should have >= 2 wings, got %d", len(goNode.Wings))
	}
	// Verify edge fields
	for _, e := range edges {
		if e.WingA == "" || e.WingB == "" {
			t.Errorf("edge missing wing: %+v", e)
		}
		if e.Room == "" {
			t.Errorf("edge missing room: %+v", e)
		}
	}
}

func TestTraverse_MaxHopsZero(t *testing.T) {
	p := setupGraphPalace(t)
	results, err := graph.Traverse(p, "golang", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Errorf("maxHops=0 should return only start room, got %d", len(results))
	}
	if results[0].Room != "golang" {
		t.Errorf("expected golang, got %s", results[0].Room)
	}
	if results[0].Hop != 0 {
		t.Errorf("expected hop=0, got %d", results[0].Hop)
	}
}

func TestFindTunnels_WithWingFilter(t *testing.T) {
	p := setupGraphPalace(t)
	tunnels, err := graph.FindTunnels(p, "wing_code", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tunnel := range tunnels {
		found := false
		for _, w := range tunnel.Wings {
			if w == "wing_code" {
				found = true
			}
		}
		if !found {
			t.Errorf("tunnel %s missing wing_code", tunnel.Room)
		}
	}
}

func TestFindTunnels_NoMatch(t *testing.T) {
	p := setupGraphPalace(t)
	tunnels, err := graph.FindTunnels(p, "wing_nonexistent", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) != 0 {
		t.Errorf("expected no tunnels for nonexistent wing, got %d", len(tunnels))
	}
}

func TestFindTunnels_BothWings(t *testing.T) {
	p := setupGraphPalace(t)
	tunnels, err := graph.FindTunnels(p, "wing_code", "wing_myproject")
	if err != nil {
		t.Fatal(err)
	}
	if len(tunnels) < 1 {
		t.Error("expected tunnels connecting both wings")
	}
	for _, tunnel := range tunnels {
		hasA, hasB := false, false
		for _, w := range tunnel.Wings {
			if w == "wing_code" {
				hasA = true
			}
			if w == "wing_myproject" {
				hasB = true
			}
		}
		if !hasA || !hasB {
			t.Errorf("tunnel %s missing one of the wings: %v", tunnel.Room, tunnel.Wings)
		}
	}
}

func TestStats_EmptyGraph(t *testing.T) {
	p, err := palace.Open(filepath.Join(t.TempDir(), "empty.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	stats, err := graph.Stats(p)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalRooms != 0 {
		t.Errorf("TotalRooms: got %d, want 0", stats.TotalRooms)
	}
	if stats.TunnelRooms != 0 {
		t.Errorf("TunnelRooms: got %d, want 0", stats.TunnelRooms)
	}
	if stats.TotalEdges != 0 {
		t.Errorf("TotalEdges: got %d, want 0", stats.TotalEdges)
	}
}

func TestStats_RoomsPerWing(t *testing.T) {
	p := setupGraphPalace(t)
	stats, err := graph.Stats(p)
	if err != nil {
		t.Fatal(err)
	}
	// wing_code has golang, python, rust = 3 rooms
	if stats.RoomsPerWing["wing_code"] != 3 {
		t.Errorf("wing_code rooms: got %d, want 3", stats.RoomsPerWing["wing_code"])
	}
	// wing_myproject has golang, rust = 2 rooms
	if stats.RoomsPerWing["wing_myproject"] != 2 {
		t.Errorf("wing_myproject rooms: got %d, want 2", stats.RoomsPerWing["wing_myproject"])
	}
}

func TestFuzzyMatch_ExactSubstring(t *testing.T) {
	p := setupGraphPalace(t)
	_, err := graph.Traverse(p, "golan", 2)
	if err == nil {
		t.Fatal("expected error for partial room name")
	}
	te, ok := err.(*graph.TraverseError)
	if !ok {
		t.Fatalf("expected TraverseError, got %T", err)
	}
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

func TestFuzzyMatch_NoMatch(t *testing.T) {
	p := setupGraphPalace(t)
	_, err := graph.Traverse(p, "zzzzz", 2)
	if err == nil {
		t.Fatal("expected error")
	}
	te, ok := err.(*graph.TraverseError)
	if !ok {
		t.Fatalf("expected TraverseError, got %T", err)
	}
	if len(te.Suggestions) != 0 {
		t.Errorf("expected 0 suggestions for 'zzzzz', got %v", te.Suggestions)
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
