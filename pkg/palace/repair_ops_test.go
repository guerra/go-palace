package palace_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/palace"
)

func TestTouchLastAccessed_BumpsMetadata(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "hello")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	before := time.Now().UTC()
	if err := p.TouchLastAccessed([]string{d.ID}); err != nil {
		t.Fatalf("touch: %v", err)
	}
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"source_file": "a.md"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 drawer, got %d", len(got))
	}
	raw, ok := got[0].Metadata["last_accessed"].(string)
	if !ok {
		t.Fatalf("last_accessed missing or not string: %+v", got[0].Metadata)
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		t.Fatalf("parse last_accessed %q: %v", raw, err)
	}
	if ts.Before(before.Add(-time.Second)) || ts.After(time.Now().Add(time.Second)) {
		t.Errorf("last_accessed %v not near now (%v)", ts, before)
	}
}

func TestTouchLastAccessed_EmptyIsNoop(t *testing.T) {
	p := openTest(t)
	if err := p.TouchLastAccessed(nil); err != nil {
		t.Errorf("nil ids: %v", err)
	}
	if err := p.TouchLastAccessed([]string{}); err != nil {
		t.Errorf("empty ids: %v", err)
	}
}

func TestColdDrawerIDs_FallbackToFiledAt(t *testing.T) {
	p := openTest(t)
	// Seed with explicit filed_at 60 days ago.
	oldT := time.Now().Add(-60 * 24 * time.Hour)
	d := makeDrawer("w", halls.HallKnowledge, "r", "old.md", 0, "aged")
	d.FiledAt = oldT
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert old: %v", err)
	}
	// Fresh drawer (today).
	fresh := makeDrawer("w", halls.HallKnowledge, "r", "new.md", 0, "fresh")
	fresh.FiledAt = time.Now()
	if err := p.Upsert(fresh); err != nil {
		t.Fatalf("upsert fresh: %v", err)
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	ids, err := p.ColdDrawerIDs(cutoff, 0, nil)
	if err != nil {
		t.Fatalf("cold: %v", err)
	}
	if len(ids) != 1 || ids[0] != d.ID {
		t.Errorf("cold ids = %v; want [%s]", ids, d.ID)
	}
}

func TestColdDrawerIDs_ProtectedHallsExcluded(t *testing.T) {
	p := openTest(t)
	oldT := time.Now().Add(-60 * 24 * time.Hour)
	// Seed one cold in diary (protected) and one in conversations (not).
	diary := makeDrawer("w", halls.HallDiary, "r", "d.md", 0, "diary")
	diary.FiledAt = oldT
	conv := makeDrawer("w", halls.HallConversations, "r", "c.md", 0, "chat")
	conv.FiledAt = oldT
	if err := p.UpsertBatch([]palace.Drawer{diary, conv}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	ids, err := p.ColdDrawerIDs(cutoff, 0, []string{halls.HallDiary})
	if err != nil {
		t.Fatalf("cold: %v", err)
	}
	if len(ids) != 1 || ids[0] != conv.ID {
		t.Errorf("cold ids = %v; want [%s] (diary must be excluded)", ids, conv.ID)
	}
}

func TestColdDrawerIDs_LimitCapsResults(t *testing.T) {
	p := openTest(t)
	oldT := time.Now().Add(-60 * 24 * time.Hour)
	var ds []palace.Drawer
	for i := 0; i < 5; i++ {
		d := makeDrawer("w", halls.HallKnowledge, "r", "f.md", i, "x")
		d.FiledAt = oldT
		ds = append(ds, d)
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	cutoff := time.Now().Add(-30 * 24 * time.Hour)
	ids, err := p.ColdDrawerIDs(cutoff, 2, nil)
	if err != nil {
		t.Fatalf("cold: %v", err)
	}
	if len(ids) != 2 {
		t.Errorf("len(ids) = %d; want 2 (limit capped)", len(ids))
	}
}

func TestArchiveDrawers_UpdatesHall(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "content")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	n, err := p.ArchiveDrawers([]string{d.ID})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	if n != 1 {
		t.Errorf("archived count = %d; want 1", n)
	}
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"hall": halls.HallArchived}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].ID != d.ID {
		t.Errorf("expected archived drawer via Get; got %+v", got)
	}
}

func TestIntegrityCheck_FreshPalaceClean(t *testing.T) {
	p := openTest(t)
	issues, err := p.IntegrityCheck()
	if err != nil {
		t.Fatalf("integrity: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %v; want none", issues)
	}
}

func TestQuickCheck_FreshPalaceClean(t *testing.T) {
	p := openTest(t)
	issues, err := p.QuickCheck()
	if err != nil {
		t.Fatalf("quick_check: %v", err)
	}
	if len(issues) != 0 {
		t.Errorf("issues = %v; want none", issues)
	}
}

func TestScanOrphans_CleanPalace(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "clean")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	dOrph, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(dOrph) != 0 || len(vOrph) != 0 {
		t.Errorf("orphans = (%v, %v); want (nil, nil)", dOrph, vOrph)
	}
}

func TestScanOrphans_VecOrphan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "x")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Raw-SQL: delete only from drawers, leaving vec orphan.
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers WHERE id = ?`, d.ID); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	_ = raw.Close()

	dOrph, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(dOrph) != 0 {
		t.Errorf("drawer orphans = %v; want none", dOrph)
	}
	if len(vOrph) != 1 || vOrph[0] != d.ID {
		t.Errorf("vec orphans = %v; want [%s]", vOrph, d.ID)
	}
}

func TestScanOrphans_DrawerOrphan(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "x")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers_vec WHERE id = ?`, d.ID); err != nil {
		t.Fatalf("raw delete vec: %v", err)
	}
	_ = raw.Close()

	dOrph, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(vOrph) != 0 {
		t.Errorf("vec orphans = %v; want none", vOrph)
	}
	if len(dOrph) != 1 || dOrph[0] != d.ID {
		t.Errorf("drawer orphans = %v; want [%s]", dOrph, d.ID)
	}
}

func TestDeleteOrphans_RemovesBoth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	d1 := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "x")
	d2 := makeDrawer("w", halls.HallKnowledge, "r", "b.md", 0, "y")
	if err := p.UpsertBatch([]palace.Drawer{d1, d2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	// d1 becomes vec orphan; d2 becomes drawer orphan.
	if _, err := raw.Exec(`DELETE FROM drawers WHERE id = ?`, d1.ID); err != nil {
		t.Fatalf("raw delete drawers: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers_vec WHERE id = ?`, d2.ID); err != nil {
		t.Fatalf("raw delete vec: %v", err)
	}
	_ = raw.Close()

	dOrph, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	n, err := p.DeleteOrphans(dOrph, vOrph)
	if err != nil {
		t.Fatalf("delete orphans: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2", n)
	}
	dOrph2, vOrph2, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan2: %v", err)
	}
	if len(dOrph2) != 0 || len(vOrph2) != 0 {
		t.Errorf("post-delete orphans = (%v, %v); want clean", dOrph2, vOrph2)
	}
}

func TestEmbeddingDim_ReadsStored(t *testing.T) {
	p := openTest(t)
	n, err := p.EmbeddingDim()
	if err != nil {
		t.Fatalf("dim: %v", err)
	}
	if n != palace.DefaultEmbeddingDim {
		t.Errorf("stored dim = %d; want %d", n, palace.DefaultEmbeddingDim)
	}
}

func TestProbeEmbedderDim_MatchesEmbedder(t *testing.T) {
	p := openTest(t)
	if got := p.ProbeEmbedderDim(); got != palace.DefaultEmbeddingDim {
		t.Errorf("probe = %d; want %d", got, palace.DefaultEmbeddingDim)
	}
}

func TestDeleteBatch_RemovesBothTables(t *testing.T) {
	p := openTest(t)
	ds := []palace.Drawer{
		makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "x"),
		makeDrawer("w", halls.HallKnowledge, "r", "b.md", 0, "y"),
		makeDrawer("w", halls.HallKnowledge, "r", "c.md", 0, "z"),
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Delete two of three.
	n, err := p.DeleteBatch([]string{ds[0].ID, ds[1].ID})
	if err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if n != 2 {
		t.Errorf("deleted = %d; want 2", n)
	}
	count, err := p.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("remaining = %d; want 1", count)
	}
	// Orphan scan must stay clean — vec rows for deleted drawers are gone.
	dOrph, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(dOrph) != 0 || len(vOrph) != 0 {
		t.Errorf("orphans after batch delete = (%v, %v); want clean", dOrph, vOrph)
	}
}

func TestDeleteBatch_EmptyIsNoop(t *testing.T) {
	p := openTest(t)
	n, err := p.DeleteBatch(nil)
	if err != nil {
		t.Errorf("nil ids: %v", err)
	}
	if n != 0 {
		t.Errorf("nil ids deleted = %d; want 0", n)
	}
	n, err = p.DeleteBatch([]string{})
	if err != nil {
		t.Errorf("empty ids: %v", err)
	}
	if n != 0 {
		t.Errorf("empty ids deleted = %d; want 0", n)
	}
}

func TestDeleteBatch_MissingIDsIgnored(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "x")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Mix present + absent ids; RowsAffected counts only present rows.
	n, err := p.DeleteBatch([]string{d.ID, "drawer_nope_0", "drawer_nope_1"})
	if err != nil {
		t.Fatalf("delete batch: %v", err)
	}
	if n != 1 {
		t.Errorf("deleted = %d; want 1 (missing ids must not error)", n)
	}
}
