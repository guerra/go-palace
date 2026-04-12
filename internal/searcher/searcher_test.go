package searcher_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-palace/internal/embed"
	"go-palace/internal/palace"
	"go-palace/internal/searcher"
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
			ID:       palace.ComputeDrawerID("proj_a", "docs", "readme.md", 0),
			Document: "This is the project documentation about testing patterns.",
			Wing:     "proj_a", Room: "docs", SourceFile: "readme.md",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
		},
		{
			ID:       palace.ComputeDrawerID("proj_a", "code", "main.go", 0),
			Document: "The main entry point of the application handles routing.",
			Wing:     "proj_a", Room: "code", SourceFile: "main.go",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
		},
		{
			ID:       palace.ComputeDrawerID("proj_b", "notes", "design.md", 0),
			Document: "Design notes for the API architecture and database schema.",
			Wing:     "proj_b", Room: "notes", SourceFile: "/path/to/design.md",
			ChunkIndex: 0, AddedBy: "test", FiledAt: time.Now(),
		},
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	return p
}

func TestSearchFormattedOutput(t *testing.T) {
	p := setupTestPalace(t)
	var buf bytes.Buffer
	opts := searcher.SearchOptions{Query: "testing", NResults: 5}
	if err := searcher.Search(p, opts, &buf); err != nil {
		t.Fatalf("search: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"Results for:", "Match:", "Source:", "\u2500"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

func TestSearchFilteredByWing(t *testing.T) {
	p := setupTestPalace(t)
	var buf bytes.Buffer
	opts := searcher.SearchOptions{Query: "test", Wing: "proj_a", NResults: 10}
	if err := searcher.Search(p, opts, &buf); err != nil {
		t.Fatalf("search: %v", err)
	}
	out := buf.String()
	if strings.Contains(out, "proj_b") {
		t.Errorf("wing filter leaked proj_b:\n%s", out)
	}
	if !strings.Contains(out, "Wing: proj_a") {
		t.Errorf("missing wing label:\n%s", out)
	}
}

func TestSearchFilteredByRoom(t *testing.T) {
	p := setupTestPalace(t)
	var buf bytes.Buffer
	opts := searcher.SearchOptions{Query: "test", Room: "docs", NResults: 10}
	if err := searcher.Search(p, opts, &buf); err != nil {
		t.Fatalf("search: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "Room: docs") {
		t.Errorf("missing room label:\n%s", out)
	}
}

func TestSearchEmptyResults(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "empty.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = p.Close() }()

	var buf bytes.Buffer
	opts := searcher.SearchOptions{Query: "anything", NResults: 5}
	if err := searcher.Search(p, opts, &buf); err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(buf.String(), "No results found") {
		t.Errorf("expected 'No results found':\n%s", buf.String())
	}
}

func TestSearchMemoriesStructured(t *testing.T) {
	p := setupTestPalace(t)
	results, err := searcher.SearchMemories(p, searcher.SearchOptions{Query: "test", NResults: 5})
	if err != nil {
		t.Fatalf("search_memories: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	for _, r := range results {
		if r.Text == "" {
			t.Error("empty text")
		}
		if r.Wing == "" {
			t.Error("empty wing")
		}
		if r.Room == "" {
			t.Error("empty room")
		}
		if r.SourceFile == "" {
			t.Error("empty source_file")
		}
	}
}

func TestSearchMemoriesEmpty(t *testing.T) {
	p, err := palace.Open(
		filepath.Join(t.TempDir(), "empty.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim),
	)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() { _ = p.Close() }()

	results, err := searcher.SearchMemories(p, searcher.SearchOptions{Query: "anything"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty, got %d", len(results))
	}
}

func TestSearchSourceFileBasename(t *testing.T) {
	p := setupTestPalace(t)
	results, err := searcher.SearchMemories(p, searcher.SearchOptions{Query: "design API", NResults: 5})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	for _, r := range results {
		if strings.Contains(r.SourceFile, "/") {
			t.Errorf("source_file should be basename only, got %q", r.SourceFile)
		}
	}
}
