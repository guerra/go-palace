package layers_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-palace/internal/embed"
	"go-palace/internal/layers"
	"go-palace/internal/palace"
)

func setupTestPalace(t *testing.T) *palace.Palace {
	t.Helper()
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "test.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatalf("open palace: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	drawers := []palace.Drawer{
		{
			ID:       palace.ComputeDrawerID("proj", "docs", "readme.md", 0),
			Document: "Important documentation about the project architecture.",
			Wing:     "proj", Room: "docs", SourceFile: "readme.md",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"importance": 5.0},
		},
		{
			ID:       palace.ComputeDrawerID("proj", "code", "main.go", 0),
			Document: "The main entry point handles CLI arguments and routing.",
			Wing:     "proj", Room: "code", SourceFile: "main.go",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"importance": 3.0},
		},
		{
			ID:       palace.ComputeDrawerID("proj", "code", "handler.go", 0),
			Document: "HTTP handler for the API endpoints.",
			Wing:     "proj", Room: "code", SourceFile: "handler.go",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"importance": 2.0},
		},
		{
			ID:       palace.ComputeDrawerID("other", "notes", "design.md", 0),
			Document: "Design notes for the database schema and migration plan.",
			Wing:     "other", Room: "notes", SourceFile: "design.md",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"importance": 4.0},
		},
		{
			ID:       palace.ComputeDrawerID("proj", "docs", "guide.md", 0),
			Document: "User guide with examples and best practices for integration.",
			Wing:     "proj", Room: "docs", SourceFile: "guide.md",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"importance": 1.0},
		},
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return p
}

func writeIdentity(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "identity.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestL0WithIdentity(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "I am Atlas, a personal AI assistant.")
	stack := layers.NewStack(p, idPath)
	got := stack.L0()
	if !strings.Contains(got, "Atlas") {
		t.Errorf("L0 missing identity content: %q", got)
	}
}

func TestL0NoIdentity(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "nonexistent.txt"))
	got := stack.L0()
	if !strings.Contains(got, "No identity configured") {
		t.Errorf("L0 missing default: %q", got)
	}
}

func TestL0Cached(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "cached identity")
	stack := layers.NewStack(p, idPath)
	first := stack.L0()
	// Overwrite file — should return cached value.
	_ = os.WriteFile(idPath, []byte("changed"), 0o644)
	second := stack.L0()
	if first != second {
		t.Errorf("L0 not cached: first=%q second=%q", first, second)
	}
}

func TestL1TopDrawers(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if !strings.Contains(got, "## L1") {
		t.Errorf("missing L1 header: %q", got)
	}
	if !strings.Contains(got, "ESSENTIAL STORY") {
		t.Errorf("missing ESSENTIAL STORY: %q", got)
	}
}

func TestL1GroupByRoom(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if !strings.Contains(got, "[code]") && !strings.Contains(got, "[docs]") {
		t.Errorf("L1 missing room groupings: %q", got)
	}
}

func TestL1Wing(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1Wing("proj")
	if strings.Contains(got, "design.md") {
		t.Errorf("L1Wing leaked other wing drawer: %q", got)
	}
	if !strings.Contains(got, "## L1") {
		t.Errorf("missing L1 header: %q", got)
	}
}

func TestL1Empty(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "empty.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = p.Close() }()
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if !strings.Contains(got, "No memories yet") {
		t.Errorf("L1 empty missing message: %q", got)
	}
}

func TestL1MaxChars(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "big.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = p.Close() }()

	// Insert many drawers with long text.
	var drawers []palace.Drawer
	for i := 0; i < 20; i++ {
		drawers = append(drawers, palace.Drawer{
			ID:         palace.ComputeDrawerID("w", "r", "f.md", i),
			Document:   strings.Repeat("word ", 60),
			Wing:       "w",
			Room:       "r",
			SourceFile: "f.md",
			ChunkIndex: i,
			AddedBy:    "test",
			FiledAt:    time.Now(),
			Metadata:   map[string]any{"importance": 5.0},
		})
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if len(got) > layers.MaxChars+500 { // some header overhead tolerance
		t.Errorf("L1 exceeded MaxChars: len=%d", len(got))
	}
}

func TestL2Retrieve(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("proj", "")
	if !strings.Contains(got, "## L2") {
		t.Errorf("missing L2 header: %q", got)
	}
	if !strings.Contains(got, "ON-DEMAND") {
		t.Errorf("missing ON-DEMAND: %q", got)
	}
}

func TestL2Empty(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("nonexistent_wing", "")
	if !strings.Contains(got, "No drawers found") {
		t.Errorf("L2 empty missing message: %q", got)
	}
}

func TestL3Search(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("documentation", "", "")
	if !strings.Contains(got, "## L3") {
		t.Errorf("missing L3 header: %q", got)
	}
	if !strings.Contains(got, "SEARCH RESULTS") {
		t.Errorf("missing SEARCH RESULTS: %q", got)
	}
	if !strings.Contains(got, "sim=") {
		t.Errorf("missing similarity: %q", got)
	}
}

func TestWakeUp(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "I am Atlas.")
	stack := layers.NewStack(p, idPath)
	got := stack.WakeUp()
	if !strings.Contains(got, "Atlas") {
		t.Errorf("WakeUp missing L0: %q", got)
	}
	if !strings.Contains(got, "## L1") {
		t.Errorf("WakeUp missing L1: %q", got)
	}
}

func TestWakeUpWing(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "I am Atlas.")
	stack := layers.NewStack(p, idPath)
	got := stack.WakeUpWing("proj")
	if !strings.Contains(got, "Atlas") {
		t.Errorf("WakeUpWing missing L0: %q", got)
	}
	if !strings.Contains(got, "## L1") {
		t.Errorf("WakeUpWing missing L1: %q", got)
	}
}

func TestTokenEstimate(t *testing.T) {
	got := layers.TokenEstimate("hello world!") // 12 chars -> 3
	if got != 3 {
		t.Errorf("TokenEstimate = %d, want 3", got)
	}
}
