package repair_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/palace"
	"github.com/guerra/go-palace/pkg/repair"
)

// openPalace returns a fresh palace backed by a throwaway file. Returns the
// palace plus the on-disk path so tests can open raw sql.DB handles for
// fault injection.
func openPalace(t *testing.T) (*palace.Palace, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open palace: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p, path
}

func mkDrawer(src string, chunk int, doc string) palace.Drawer {
	return palace.Drawer{
		ID:         palace.ComputeDrawerID("w", "r", src, chunk),
		Document:   doc,
		Wing:       "w",
		Hall:       halls.HallKnowledge,
		Room:       "r",
		SourceFile: src,
		ChunkIndex: chunk,
		AddedBy:    "test",
		FiledAt:    time.Now(),
	}
}

func TestRepairCleanPalace(t *testing.T) {
	p, _ := openPalace(t)
	if err := p.Upsert(mkDrawer("a.md", 0, "hello")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rep, err := repair.Repair(p, repair.RepairOptions{})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.IntegrityIssues) != 0 {
		t.Errorf("IntegrityIssues = %v; want none", rep.IntegrityIssues)
	}
	if len(rep.DrawerOrphans) != 0 || len(rep.VecOrphans) != 0 {
		t.Errorf("orphans = (%v, %v); want none", rep.DrawerOrphans, rep.VecOrphans)
	}
	if rep.DimMismatch != nil {
		t.Errorf("DimMismatch = %+v; want nil", rep.DimMismatch)
	}
	if rep.OrphansDeleted != 0 {
		t.Errorf("OrphansDeleted = %d; want 0", rep.OrphansDeleted)
	}
}

func TestRepairVecOrphan(t *testing.T) {
	p, path := openPalace(t)
	d := mkDrawer("a.md", 0, "x")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Raw-SQL: delete the drawer row, leaving drawers_vec orphan.
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers WHERE id = ?`, d.ID); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	_ = raw.Close()

	rep, err := repair.Repair(p, repair.RepairOptions{})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.DrawerOrphans) != 0 {
		t.Errorf("DrawerOrphans = %v; want none", rep.DrawerOrphans)
	}
	if len(rep.VecOrphans) != 1 || rep.VecOrphans[0] != d.ID {
		t.Errorf("VecOrphans = %v; want [%s]", rep.VecOrphans, d.ID)
	}
}

func TestRepairDrawerOrphan(t *testing.T) {
	p, path := openPalace(t)
	d := mkDrawer("a.md", 0, "x")
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

	rep, err := repair.Repair(p, repair.RepairOptions{})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.VecOrphans) != 0 {
		t.Errorf("VecOrphans = %v; want none", rep.VecOrphans)
	}
	if len(rep.DrawerOrphans) != 1 || rep.DrawerOrphans[0] != d.ID {
		t.Errorf("DrawerOrphans = %v; want [%s]", rep.DrawerOrphans, d.ID)
	}
}

func TestRepairAutoDelete(t *testing.T) {
	p, path := openPalace(t)
	d1 := mkDrawer("a.md", 0, "x")
	d2 := mkDrawer("b.md", 0, "y")
	if err := p.UpsertBatch([]palace.Drawer{d1, d2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers WHERE id = ?`, d1.ID); err != nil {
		t.Fatalf("raw delete 1: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers_vec WHERE id = ?`, d2.ID); err != nil {
		t.Fatalf("raw delete 2: %v", err)
	}
	_ = raw.Close()

	rep, err := repair.Repair(p, repair.RepairOptions{Mode: repair.ModeAutoDelete})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if rep.OrphansDeleted != 2 {
		t.Errorf("OrphansDeleted = %d; want 2", rep.OrphansDeleted)
	}
	// Subsequent scan must show clean.
	rep2, err := repair.Repair(p, repair.RepairOptions{Mode: repair.ModeReportOnly})
	if err != nil {
		t.Fatalf("repair2: %v", err)
	}
	if len(rep2.DrawerOrphans) != 0 || len(rep2.VecOrphans) != 0 {
		t.Errorf("post-auto-delete orphans = (%v, %v); want clean",
			rep2.DrawerOrphans, rep2.VecOrphans)
	}
}

func TestRepairReportOnlyLeavesOrphans(t *testing.T) {
	p, path := openPalace(t)
	d := mkDrawer("a.md", 0, "x")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(`DELETE FROM drawers WHERE id = ?`, d.ID); err != nil {
		t.Fatalf("raw delete: %v", err)
	}
	_ = raw.Close()

	rep, err := repair.Repair(p, repair.RepairOptions{Mode: repair.ModeReportOnly})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.VecOrphans) != 1 {
		t.Fatalf("VecOrphans = %v; want 1 orphan", rep.VecOrphans)
	}
	if rep.OrphansDeleted != 0 {
		t.Errorf("ModeReportOnly deleted %d orphans; want 0", rep.OrphansDeleted)
	}
	// Orphan still present.
	_, vOrph, err := p.ScanOrphans()
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(vOrph) != 1 {
		t.Errorf("orphan vanished after report-only: %v", vOrph)
	}
}

func TestRepairDimMismatch(t *testing.T) {
	p, path := openPalace(t)
	if err := p.Upsert(mkDrawer("a.md", 0, "x")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	// Tamper with palace_meta to simulate a palace that claims a different dim.
	raw, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	if _, err := raw.Exec(
		`UPDATE palace_meta SET value = '768' WHERE key = 'embedding_dim'`); err != nil {
		t.Fatalf("raw update: %v", err)
	}
	_ = raw.Close()

	rep, err := repair.Repair(p, repair.RepairOptions{})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if rep.DimMismatch == nil {
		t.Fatal("expected DimMismatch, got nil")
	}
	if rep.DimMismatch.Stored != 768 {
		t.Errorf("Stored = %d; want 768", rep.DimMismatch.Stored)
	}
	if rep.DimMismatch.Embedder != palace.DefaultEmbeddingDim {
		t.Errorf("Embedder = %d; want %d", rep.DimMismatch.Embedder, palace.DefaultEmbeddingDim)
	}
}

func TestRepairSkipIntegrityCheck(t *testing.T) {
	p, _ := openPalace(t)
	if err := p.Upsert(mkDrawer("a.md", 0, "x")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rep, err := repair.Repair(p, repair.RepairOptions{SkipIntegrityCheck: true})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.IntegrityIssues) != 0 {
		t.Errorf("SkipIntegrityCheck set but got issues: %v", rep.IntegrityIssues)
	}
}

func TestRepairQuickCheckOnly(t *testing.T) {
	p, _ := openPalace(t)
	if err := p.Upsert(mkDrawer("a.md", 0, "x")); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	rep, err := repair.Repair(p, repair.RepairOptions{QuickCheck: true})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rep.IntegrityIssues) != 0 {
		t.Errorf("quick_check: issues = %v; want none", rep.IntegrityIssues)
	}
}
