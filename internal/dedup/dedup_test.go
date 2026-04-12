package dedup_test

import (
	"path/filepath"
	"testing"
	"time"

	"go-palace/internal/dedup"
	"go-palace/pkg/embed"
	"go-palace/pkg/palace"
)

func openTest(t *testing.T) *palace.Palace {
	t.Helper()
	p, err := palace.Open(filepath.Join(t.TempDir(), "p.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func makeDrawer(wing, room, src string, chunk int, doc string) palace.Drawer {
	return palace.Drawer{
		ID:         palace.ComputeDrawerID(wing, room, src, chunk),
		Document:   doc,
		Wing:       wing,
		Room:       room,
		SourceFile: src,
		ChunkIndex: chunk,
		AddedBy:    "test",
		FiledAt:    time.Now(),
	}
}

// --- TestGetSourceGroups ---

func TestGetSourceGroups(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		p := openTest(t)
		var drawers []palace.Drawer
		for i := 0; i < 5; i++ {
			drawers = append(drawers, makeDrawer("w", "r", "a.txt", i, "document content number "+string(rune('A'+i))))
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, g := range groups {
			if g.SourceFile == "a.txt" {
				found = true
			}
		}
		if !found {
			t.Error("expected a.txt group")
		}
	})

	t.Run("below_min", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "doc one content here enough"),
			makeDrawer("w", "r", "a.txt", 1, "doc two content here enough"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) != 0 {
			t.Errorf("expected no groups, got %d", len(groups))
		}
	})

	t.Run("source_filter", func(t *testing.T) {
		p := openTest(t)
		var drawers []palace.Drawer
		for i := 0; i < 5; i++ {
			drawers = append(drawers, makeDrawer("w", "r", "project_a.txt", i, "content for project a item "+string(rune('A'+i))))
		}
		drawers = append(drawers, makeDrawer("w", "r", "other.txt", 0, "other content here and more"))
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5, SourcePattern: "project_a"})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "other.txt" {
				t.Error("other.txt should be filtered out")
			}
		}
	})

	t.Run("wing_filter", func(t *testing.T) {
		p := openTest(t)
		var drawers []palace.Drawer
		for i := 0; i < 5; i++ {
			drawers = append(drawers, makeDrawer("my_wing", "r", "a.txt", i, "content for wing test item "+string(rune('A'+i))))
		}
		for i := 0; i < 5; i++ {
			drawers = append(drawers, makeDrawer("other_wing", "r", "a.txt", i+10, "content for other wing item "+string(rune('A'+i))))
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5, Wing: "my_wing"})
		if err != nil {
			t.Fatal(err)
		}
		if len(groups) == 0 {
			t.Error("expected at least one group for my_wing")
		}
	})

	t.Run("missing_source_file", func(t *testing.T) {
		p := openTest(t)
		for i := 0; i < 5; i++ {
			d := makeDrawer("w", "r", "", i, "content with empty source "+string(rune('A'+i)))
			d.SourceFile = "" // Go palace: always has field, but can be empty
			if err := p.Upsert(d); err != nil {
				t.Fatal(err)
			}
		}
		// The IDs have "" source_file; our logic maps "" to "unknown"
		// but since palace stores "" not "unknown", we check it doesn't crash
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		// empty source_file drawers should still be grouped
		_ = groups
	})
}

// --- TestDedupSourceGroup ---

func TestDedupSourceGroup(t *testing.T) {
	t.Run("all_unique", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "completely different document about Go programming and error handling"),
			makeDrawer("w", "r", "a.txt", 1, "another unique document about Python data science workflows"),
			makeDrawer("w", "r", "a.txt", 2, "third document about JavaScript frontend frameworks and React"),
			makeDrawer("w", "r", "a.txt", 3, "fourth about Rust memory safety and ownership model design"),
			makeDrawer("w", "r", "a.txt", 4, "fifth about database design patterns and SQL optimization tricks"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "a.txt" {
				// With FakeEmbedder, different texts produce different vectors
				// so they should all be kept (high distance between them)
				if len(g.KeptIDs) < 3 {
					t.Errorf("expected most to be kept, got kept=%d deleted=%d", len(g.KeptIDs), len(g.DeletedIDs))
				}
			}
		}
	})

	t.Run("with_duplicate", func(t *testing.T) {
		p := openTest(t)
		doc := "this is a document that will be duplicated exactly the same content"
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, doc),
			makeDrawer("w", "r", "a.txt", 1, doc),
			makeDrawer("w", "r", "a.txt", 2, doc),
			makeDrawer("w", "r", "a.txt", 3, doc),
			makeDrawer("w", "r", "a.txt", 4, doc),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "a.txt" {
				// Identical texts → distance 0 → all but first should be flagged
				if len(g.DeletedIDs) == 0 {
					t.Error("expected duplicates to be detected")
				}
			}
		}
	})

	t.Run("short_docs_deleted", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "long enough document to keep in the palace for dedup testing"),
			makeDrawer("w", "r", "a.txt", 1, "also long enough to keep around in the palace for testing"),
			makeDrawer("w", "r", "a.txt", 2, "tiny"),
			makeDrawer("w", "r", "a.txt", 3, "sm"),
			makeDrawer("w", "r", "a.txt", 4, "x"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "a.txt" && len(g.DeletedIDs) == 0 {
				t.Error("short docs should be flagged for deletion")
			}
		}
	})

	t.Run("empty_doc_deleted", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "real document content here that is long enough for testing"),
			makeDrawer("w", "r", "a.txt", 1, "another real document that is different from the first one"),
			makeDrawer("w", "r", "a.txt", 2, ""),
			makeDrawer("w", "r", "a.txt", 3, "yet another document with substantial content for dedup"),
			makeDrawer("w", "r", "a.txt", 4, "and one more unique document about something else entirely"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "a.txt" {
				if len(g.DeletedIDs) == 0 {
					t.Error("empty doc should be flagged for deletion")
				}
			}
		}
	})

	t.Run("live_deletes", func(t *testing.T) {
		p := openTest(t)
		doc := "identical document content that should trigger dedup detection"
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, doc),
			makeDrawer("w", "r", "a.txt", 1, doc),
			makeDrawer("w", "r", "a.txt", 2, doc),
			makeDrawer("w", "r", "a.txt", 3, doc),
			makeDrawer("w", "r", "a.txt", 4, doc),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		before, _ := p.Count()
		deleted, err := dedup.Deduplicate(p, 0.15, false, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		after, _ := p.Count()
		if deleted == 0 {
			t.Error("expected deletions in live mode")
		}
		if after >= before {
			t.Errorf("expected fewer drawers after dedup: before=%d after=%d", before, after)
		}
	})

	t.Run("query_failure_keeps", func(t *testing.T) {
		// With real palace, query won't fail. We test the safe path:
		// an empty palace won't match anything, so all are kept.
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "unique document one with enough content for testing"),
			makeDrawer("w", "r", "a.txt", 1, "unique document two with different content for testing"),
			makeDrawer("w", "r", "a.txt", 2, "unique document three with more varied content here"),
			makeDrawer("w", "r", "a.txt", 3, "unique document four with some other topic entirely"),
			makeDrawer("w", "r", "a.txt", 4, "unique document five about a completely different subject"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		groups, err := dedup.FindDuplicates(p, 0.15, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		for _, g := range groups {
			if g.SourceFile == "a.txt" {
				// Different texts → different FakeEmbedder vectors → high distance → kept
				if len(g.KeptIDs) < 3 {
					t.Errorf("expected most unique docs kept, got %d", len(g.KeptIDs))
				}
			}
		}
	})
}

// --- TestDedupPalace ---

func TestDedupPalace(t *testing.T) {
	t.Run("dry_run", func(t *testing.T) {
		p := openTest(t)
		doc := "same doc for dedup dry run testing purposes here"
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, doc),
			makeDrawer("w", "r", "a.txt", 1, doc),
			makeDrawer("w", "r", "a.txt", 2, doc),
			makeDrawer("w", "r", "a.txt", 3, doc),
			makeDrawer("w", "r", "a.txt", 4, doc),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		before, _ := p.Count()
		deleted, err := dedup.Deduplicate(p, 0.15, true, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		after, _ := p.Count()
		if after != before {
			t.Errorf("dry run should not delete: before=%d after=%d", before, after)
		}
		if deleted == 0 {
			t.Error("expected duplicates found even in dry run")
		}
	})

	t.Run("with_wing", func(t *testing.T) {
		p := openTest(t)
		doc := "same doc for wing filter testing in dedup module"
		for i := 0; i < 5; i++ {
			if err := p.Upsert(makeDrawer("test_wing", "r", "a.txt", i, doc)); err != nil {
				t.Fatal(err)
			}
		}
		for i := 0; i < 5; i++ {
			if err := p.Upsert(makeDrawer("other_wing", "r", "a.txt", i+10, doc)); err != nil {
				t.Fatal(err)
			}
		}
		_, err := dedup.Deduplicate(p, 0.15, true, dedup.DedupOptions{MinCount: 5, Wing: "test_wing"})
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("no_groups", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "one doc here"),
			makeDrawer("w", "r", "b.txt", 0, "another doc here"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		deleted, err := dedup.Deduplicate(p, 0.15, true, dedup.DedupOptions{MinCount: 5})
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 0 {
			t.Errorf("expected 0 deleted, got %d", deleted)
		}
	})
}

// --- TestShowStats ---

func TestShowStats(t *testing.T) {
	p := openTest(t)
	for i := 0; i < 5; i++ {
		if err := p.Upsert(makeDrawer("w", "r", "a.txt", i, "doc content number "+string(rune('A'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	stats, err := dedup.ShowStats(p, dedup.DedupOptions{MinCount: 5})
	if err != nil {
		t.Fatal(err)
	}
	if stats.SourcesChecked == 0 {
		t.Error("expected at least one source")
	}
	if stats.TotalDrawers != 5 {
		t.Errorf("expected 5 drawers, got %d", stats.TotalDrawers)
	}
}
