package repair_test

import (
	"path/filepath"
	"testing"
	"time"

	"go-palace/internal/embed"
	"go-palace/internal/palace"
	"go-palace/internal/repair"
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

// --- TestPalacePath (adapted: repair accepts palace directly) ---

func TestPalaceInjection(t *testing.T) {
	t.Run("accepts_palace_directly", func(t *testing.T) {
		p := openTest(t)
		result, err := repair.Repair(p, true)
		if err != nil {
			t.Fatal(err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
	})

	t.Run("works_with_temp_palace", func(t *testing.T) {
		p := openTest(t)
		path := p.Path()
		if path == "" {
			t.Error("expected non-empty path")
		}
	})
}

// --- TestPaginateIDs (adapted: test scan pagination) ---

func TestScanPagination(t *testing.T) {
	t.Run("single_batch", func(t *testing.T) {
		p := openTest(t)
		if err := p.Upsert(makeDrawer("w", "r", "a.txt", 0, "test doc")); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if scan.Total != 1 {
			t.Errorf("expected 1, got %d", scan.Total)
		}
		if len(scan.GoodIDs) != 1 {
			t.Errorf("expected 1 good, got %d", len(scan.GoodIDs))
		}
	})

	t.Run("empty", func(t *testing.T) {
		p := openTest(t)
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if scan.Total != 0 {
			t.Errorf("expected 0, got %d", scan.Total)
		}
	})

	t.Run("with_wing_filter", func(t *testing.T) {
		p := openTest(t)
		if err := p.Upsert(makeDrawer("w1", "r", "a.txt", 0, "doc for w1")); err != nil {
			t.Fatal(err)
		}
		if err := p.Upsert(makeDrawer("w2", "r", "b.txt", 0, "doc for w2")); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "w1")
		if err != nil {
			t.Fatal(err)
		}
		if scan.Total != 1 {
			t.Errorf("expected 1, got %d", scan.Total)
		}
	})

	t.Run("multiple_pages", func(t *testing.T) {
		p := openTest(t)
		var drawers []palace.Drawer
		for i := 0; i < 10; i++ {
			drawers = append(drawers, makeDrawer("w", "r", "a.txt", i, "content for pagination test"))
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if scan.Total != 10 {
			t.Errorf("expected 10, got %d", scan.Total)
		}
	})
}

// --- TestScanPalace ---

func TestScanPalace(t *testing.T) {
	t.Run("no_ids", func(t *testing.T) {
		p := openTest(t)
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(scan.GoodIDs) != 0 || len(scan.BadIDs) != 0 {
			t.Error("expected empty results")
		}
	})

	t.Run("all_good", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "good doc one"),
			makeDrawer("w", "r", "a.txt", 1, "good doc two"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(scan.GoodIDs) != 2 {
			t.Errorf("expected 2 good, got %d", len(scan.GoodIDs))
		}
		if len(scan.BadIDs) != 0 {
			t.Errorf("expected 0 bad, got %d", len(scan.BadIDs))
		}
	})

	t.Run("with_bad_ids_sqlite_vec_healthy", func(t *testing.T) {
		// In sqlite-vec, Get always succeeds for existing IDs.
		// So scan should find 0 bad IDs on a healthy palace.
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "doc one for scan"),
			makeDrawer("w", "r", "a.txt", 1, "doc two for scan"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "")
		if err != nil {
			t.Fatal(err)
		}
		if len(scan.BadIDs) != 0 {
			t.Errorf("expected 0 bad on healthy palace, got %d", len(scan.BadIDs))
		}
	})

	t.Run("with_wing_filter", func(t *testing.T) {
		p := openTest(t)
		if err := p.Upsert(makeDrawer("target", "r", "a.txt", 0, "target doc")); err != nil {
			t.Fatal(err)
		}
		if err := p.Upsert(makeDrawer("other", "r", "b.txt", 0, "other doc")); err != nil {
			t.Fatal(err)
		}
		scan, err := repair.ScanPalace(p, "target")
		if err != nil {
			t.Fatal(err)
		}
		if scan.Total != 1 {
			t.Errorf("expected 1, got %d", scan.Total)
		}
	})
}

// --- TestPruneCorrupt ---

func TestPruneCorrupt(t *testing.T) {
	t.Run("empty_bad_ids", func(t *testing.T) {
		p := openTest(t)
		n, err := repair.PruneCorrupt(p, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 0 {
			t.Errorf("expected 0, got %d", n)
		}
	})

	t.Run("dry_run", func(t *testing.T) {
		p := openTest(t)
		if err := p.Upsert(makeDrawer("w", "r", "a.txt", 0, "to prune")); err != nil {
			t.Fatal(err)
		}
		d := makeDrawer("w", "r", "a.txt", 0, "to prune")
		n, err := repair.PruneCorrupt(p, []string{d.ID}, true)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("expected 1, got %d", n)
		}
		// Verify drawer still exists.
		count, _ := p.Count()
		if count != 1 {
			t.Errorf("dry run should not delete: count=%d", count)
		}
	})

	t.Run("confirmed", func(t *testing.T) {
		p := openTest(t)
		d := makeDrawer("w", "r", "a.txt", 0, "to prune confirmed")
		if err := p.Upsert(d); err != nil {
			t.Fatal(err)
		}
		n, err := repair.PruneCorrupt(p, []string{d.ID}, false)
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 {
			t.Errorf("expected 1 deleted, got %d", n)
		}
		count, _ := p.Count()
		if count != 0 {
			t.Errorf("expected 0 after prune, got %d", count)
		}
	})

	t.Run("delete_failure_nonexistent", func(t *testing.T) {
		p := openTest(t)
		// Deleting a nonexistent ID returns ErrNotFound.
		n, err := repair.PruneCorrupt(p, []string{"nonexistent_id"}, false)
		// Should not crash; logs warning.
		if n != 0 {
			t.Errorf("expected 0 deleted, got %d", n)
		}
		_ = err
	})
}

// --- TestRebuildIndex ---

func TestRebuildIndex(t *testing.T) {
	t.Run("empty_palace", func(t *testing.T) {
		p := openTest(t)
		result, err := repair.RebuildIndex(p, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Scanned != 0 {
			t.Errorf("expected 0 scanned, got %d", result.Scanned)
		}
		if result.Rebuilt != 0 {
			t.Errorf("expected 0 rebuilt, got %d", result.Rebuilt)
		}
	})

	t.Run("dry_run", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "doc one for rebuild"),
			makeDrawer("w", "r", "a.txt", 1, "doc two for rebuild"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		result, err := repair.RebuildIndex(p, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.Rebuilt != 2 {
			t.Errorf("expected 2 rebuilt (dry), got %d", result.Rebuilt)
		}
	})

	t.Run("success", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "doc one"),
			makeDrawer("w", "r", "a.txt", 1, "doc two"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		result, err := repair.RebuildIndex(p, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Rebuilt != 2 {
			t.Errorf("expected 2 rebuilt, got %d", result.Rebuilt)
		}
		if len(result.Errors) != 0 {
			t.Errorf("unexpected errors: %v", result.Errors)
		}
	})

	t.Run("error_on_closed_palace", func(t *testing.T) {
		p, err := palace.Open(filepath.Join(t.TempDir(), "closed.db"),
			embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
		if err != nil {
			t.Fatal(err)
		}
		_ = p.Close()
		_, err = repair.RebuildIndex(p, false)
		if err == nil {
			t.Error("expected error on closed palace")
		}
	})
}

// --- TestRepair (full pipeline) ---

func TestRepairFull(t *testing.T) {
	t.Run("full_pipeline", func(t *testing.T) {
		p := openTest(t)
		drawers := []palace.Drawer{
			makeDrawer("w", "r", "a.txt", 0, "doc for full repair"),
			makeDrawer("w", "r", "a.txt", 1, "another doc for repair"),
		}
		if err := p.UpsertBatch(drawers); err != nil {
			t.Fatal(err)
		}
		result, err := repair.Repair(p, false)
		if err != nil {
			t.Fatal(err)
		}
		if result.Scanned != 2 {
			t.Errorf("expected 2 scanned, got %d", result.Scanned)
		}
		if result.Rebuilt != 2 {
			t.Errorf("expected 2 rebuilt, got %d", result.Rebuilt)
		}
	})

	t.Run("dry_run_full", func(t *testing.T) {
		p := openTest(t)
		if err := p.Upsert(makeDrawer("w", "r", "a.txt", 0, "dry run doc")); err != nil {
			t.Fatal(err)
		}
		result, err := repair.Repair(p, true)
		if err != nil {
			t.Fatal(err)
		}
		if result.Scanned != 1 {
			t.Errorf("expected 1, got %d", result.Scanned)
		}
	})
}
