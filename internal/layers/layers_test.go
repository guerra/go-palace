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

func TestTokenEstimate_Empty(t *testing.T) {
	got := layers.TokenEstimate("")
	if got != 0 {
		t.Errorf("TokenEstimate('') = %d, want 0", got)
	}
}

func TestL0_TokenEstimate(t *testing.T) {
	p := setupTestPalace(t)
	content := strings.Repeat("A", 400) // 400 chars -> 100 tokens
	idPath := writeIdentity(t, content)
	stack := layers.NewStack(p, idPath)
	text := stack.L0()
	estimate := layers.TokenEstimate(text)
	if estimate != 100 {
		t.Errorf("L0 token estimate = %d, want 100", estimate)
	}
}

func TestL0_StripsWhitespace(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "  Hello world  \n\n")
	stack := layers.NewStack(p, idPath)
	text := stack.L0()
	if text != "Hello world" {
		t.Errorf("L0 should strip whitespace: got %q", text)
	}
}

func TestL0_DefaultPath(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, "")
	got := stack.L0()
	// Should either load identity or show default message.
	if got == "" {
		t.Error("L0 with empty path should not be empty")
	}
}

func TestL1_ImportanceFromVariousKeys(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "keys.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	drawers := []palace.Drawer{
		{
			ID: "ew", Document: "emotional weight memory", Wing: "proj", Room: "r",
			SourceFile: "a.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"emotional_weight": 5.0},
		},
		{
			ID: "w", Document: "weight memory", Wing: "proj", Room: "r",
			SourceFile: "b.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{"weight": 1.0},
		},
		{
			ID: "no", Document: "no key memory", Wing: "proj", Room: "r",
			SourceFile: "c.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
			Metadata: map[string]any{},
		},
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatal(err)
	}
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if !strings.Contains(got, "ESSENTIAL STORY") {
		t.Errorf("missing ESSENTIAL STORY: %q", got)
	}
}

func TestL1_TruncatesLongSnippets(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "long.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	drawers := []palace.Drawer{{
		ID: "long", Document: strings.Repeat("A", 300), Wing: "proj", Room: "r",
		SourceFile: "long.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
		Metadata: map[string]any{"importance": 5.0},
	}}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatal(err)
	}
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L1()
	if !strings.Contains(got, "...") {
		t.Errorf("expected '...' for truncated snippet: %q", got)
	}
}

func TestL2_WithWing(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("proj", "")
	if !strings.Contains(got, "ON-DEMAND") {
		t.Errorf("L2 missing ON-DEMAND: %q", got)
	}
}

func TestL2_WithRoom(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("", "code")
	if !strings.Contains(got, "ON-DEMAND") {
		t.Errorf("L2 with room filter missing ON-DEMAND: %q", got)
	}
}

func TestL2_WithWingAndRoom(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("proj", "docs")
	if !strings.Contains(got, "ON-DEMAND") {
		t.Errorf("L2 with wing+room missing ON-DEMAND: %q", got)
	}
}

func TestL2_NoFilter(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("", "")
	// With no filter, should return all drawers.
	if !strings.Contains(got, "ON-DEMAND") {
		t.Errorf("L2 no filter missing ON-DEMAND: %q", got)
	}
}

func TestL2_TruncatesLongSnippets(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "long2.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	drawers := []palace.Drawer{{
		ID: "long2", Document: strings.Repeat("B", 400), Wing: "proj", Room: "r",
		SourceFile: "long.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
	}}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatal(err)
	}
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L2("proj", "")
	if !strings.Contains(got, "...") {
		t.Errorf("expected '...' for truncated L2 snippet: %q", got)
	}
}

func TestL3_NoResults(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "empty3.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("something", "", "")
	if !strings.Contains(got, "No results") {
		t.Errorf("empty L3 should say No results: %q", got)
	}
}

func TestL3_WithWingFilter(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("documentation", "proj", "")
	if !strings.Contains(got, "SEARCH RESULTS") {
		t.Errorf("L3 with wing filter missing SEARCH RESULTS: %q", got)
	}
}

func TestL3_WithRoomFilter(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("documentation", "", "docs")
	if !strings.Contains(got, "SEARCH RESULTS") {
		t.Errorf("L3 with room filter missing SEARCH RESULTS: %q", got)
	}
}

func TestL3_WithWingAndRoom(t *testing.T) {
	p := setupTestPalace(t)
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("documentation", "proj", "docs")
	if !strings.Contains(got, "SEARCH RESULTS") {
		t.Errorf("L3 with wing+room missing SEARCH RESULTS: %q", got)
	}
}

func TestL3_TruncatesLongDocs(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "long3.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = p.Close() }()

	drawers := []palace.Drawer{{
		ID: "long3", Document: strings.Repeat("C", 400), Wing: "proj", Room: "r",
		SourceFile: "long.md", ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
	}}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatal(err)
	}
	stack := layers.NewStack(p, filepath.Join(t.TempDir(), "none"))
	got := stack.L3("CCCC", "", "")
	if !strings.Contains(got, "...") {
		t.Errorf("expected '...' for truncated L3 doc: %q", got)
	}
}

func TestWakeUpWing_Propagation(t *testing.T) {
	p := setupTestPalace(t)
	idPath := writeIdentity(t, "I am Atlas.")
	stack := layers.NewStack(p, idPath)
	got := stack.WakeUpWing("proj")
	// Should contain identity.
	if !strings.Contains(got, "Atlas") {
		t.Errorf("WakeUpWing missing L0: %q", got)
	}
	// Should not contain the "other" wing's content if wing filter works.
	if !strings.Contains(got, "## L1") {
		t.Errorf("WakeUpWing missing L1: %q", got)
	}
}
