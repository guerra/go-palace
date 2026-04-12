package palace_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

func TestOpenCreateSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := p.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}
	p2, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open2 (re-open): %v", err)
	}
	_ = p2.Close()
}

func TestOpenNilEmbedder(t *testing.T) {
	if _, err := palace.Open(filepath.Join(t.TempDir(), "p.db"), nil); err == nil {
		t.Fatal("expected error on nil embedder")
	}
}

func TestUpsertAndCount(t *testing.T) {
	p := openTest(t)
	drawers := []palace.Drawer{
		makeDrawer("myproj", "docs", "a.md", 0, "alpha content"),
		makeDrawer("myproj", "docs", "a.md", 1, "beta content"),
		makeDrawer("other", "code", "b.go", 0, "gamma content"),
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert batch: %v", err)
	}
	got, err := p.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if got != 3 {
		t.Errorf("count: got %d want 3", got)
	}
	gotWing, err := p.CountWhere(map[string]string{"wing": "myproj"})
	if err != nil {
		t.Fatalf("count wing: %v", err)
	}
	if gotWing != 2 {
		t.Errorf("count wing=myproj: got %d want 2", gotWing)
	}
	gotMissing, err := p.CountWhere(map[string]string{"wing": "nope"})
	if err != nil {
		t.Fatalf("count missing: %v", err)
	}
	if gotMissing != 0 {
		t.Errorf("count missing wing: got %d want 0", gotMissing)
	}
}

func TestCountWhereRejectsUnknownKey(t *testing.T) {
	p := openTest(t)
	_, err := p.CountWhere(map[string]string{"DROP TABLE": "x"})
	if !errors.Is(err, palace.ErrUnknownWhereKey) {
		t.Errorf("err = %v, want ErrUnknownWhereKey", err)
	}
}

func TestGetByWhere(t *testing.T) {
	p := openTest(t)
	in := []palace.Drawer{
		makeDrawer("w", "r", "a.md", 0, "zero"),
		makeDrawer("w", "r", "a.md", 1, "one"),
		makeDrawer("w", "r", "b.md", 0, "other file"),
	}
	if err := p.UpsertBatch(in); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, err := p.Get(palace.GetOptions{
		Where: map[string]string{"source_file": "a.md"},
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0].ChunkIndex != 0 || got[1].ChunkIndex != 1 {
		t.Errorf("ordering: got %d,%d want 0,1", got[0].ChunkIndex, got[1].ChunkIndex)
	}
}

func TestGetWhereRejectsUnknownKey(t *testing.T) {
	p := openTest(t)
	_, err := p.Get(palace.GetOptions{Where: map[string]string{"DROP TABLE": "x"}})
	if err == nil {
		t.Fatal("expected error for unknown key")
	}
	if !errors.Is(err, palace.ErrUnknownWhereKey) {
		t.Errorf("err not ErrUnknownWhereKey: %v", err)
	}
	if !strings.Contains(err.Error(), "unknown where key") {
		t.Errorf("err text: %v", err)
	}
}

func TestDelete(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", "r", "a.md", 0, "content")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := p.Delete(d.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"source_file": "a.md"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty after delete, got %d rows", len(got))
	}
	if err := p.Delete(d.ID); !errors.Is(err, palace.ErrNotFound) {
		t.Errorf("second delete err = %v, want ErrNotFound", err)
	}
}

func TestQuerySemanticOrder(t *testing.T) {
	p := openTest(t)
	docs := []string{"alpha", "beta", "gamma", "delta", "epsilon"}
	var drawers []palace.Drawer
	for i, doc := range docs {
		drawers = append(drawers, makeDrawer("w", "r", "f.md", i, doc))
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	res, err := p.Query("alpha", palace.QueryOptions{NResults: 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) != 3 {
		t.Fatalf("got %d results, want 3", len(res))
	}
	// Distances must be monotonic non-decreasing (similarity non-increasing).
	for i := 1; i < len(res); i++ {
		if res[i].Similarity > res[i-1].Similarity {
			t.Errorf("result order broken at %d: %f > %f",
				i, res[i].Similarity, res[i-1].Similarity)
		}
	}
}

func TestQueryWingFilter(t *testing.T) {
	p := openTest(t)
	drawers := []palace.Drawer{
		makeDrawer("A", "r1", "a.md", 0, "lorem"),
		makeDrawer("A", "r1", "a.md", 1, "ipsum"),
		makeDrawer("B", "r2", "b.md", 0, "dolor"),
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	res, err := p.Query("x", palace.QueryOptions{Wing: "A", NResults: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("no results with wing filter")
	}
	for _, r := range res {
		if r.Drawer.Wing != "A" {
			t.Errorf("wing leak: %q", r.Drawer.Wing)
		}
	}
}

func TestDrawerIDIsDeterministic(t *testing.T) {
	id1 := palace.ComputeDrawerID("w", "r", "a.md", 0)
	id2 := palace.ComputeDrawerID("w", "r", "a.md", 0)
	if id1 != id2 {
		t.Errorf("non-deterministic: %q vs %q", id1, id2)
	}
	if !strings.HasPrefix(id1, "drawer_w_r_") {
		t.Errorf("bad prefix: %q", id1)
	}
	suffix := strings.TrimPrefix(id1, "drawer_w_r_")
	if len(suffix) != 24 {
		t.Errorf("suffix len: got %d want 24 (id=%q)", len(suffix), id1)
	}
	idOther := palace.ComputeDrawerID("w", "r", "a.md", 1)
	if idOther == id1 {
		t.Errorf("chunk index not part of id")
	}
}

func TestPersistenceAcrossClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p1, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := p1.UpsertBatch([]palace.Drawer{
		makeDrawer("w", "r", "a.md", 0, "one"),
		makeDrawer("w", "r", "a.md", 1, "two"),
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := p1.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	p2, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer func() { _ = p2.Close() }()
	n, err := p2.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 2 {
		t.Errorf("after reopen count: got %d want 2", n)
	}
}

func TestOpenDimensionMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(384))
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	_ = p.Close()

	_, err = palace.Open(path, embed.NewFakeEmbedder(128))
	if !errors.Is(err, palace.ErrDimensionMismatch) {
		t.Fatalf("expected ErrDimensionMismatch, got: %v", err)
	}
}

func TestOpenSameDimensionSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.Open(path, embed.NewFakeEmbedder(384))
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	_ = p.Close()

	p2, err := palace.Open(path, embed.NewFakeEmbedder(384))
	if err != nil {
		t.Fatalf("open2 should succeed with same dim: %v", err)
	}
	_ = p2.Close()
}

func TestUpsertReplacesRow(t *testing.T) {
	p := openTest(t)
	d := makeDrawer("w", "r", "a.md", 0, "initial")
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert1: %v", err)
	}
	d.Document = "updated"
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert2: %v", err)
	}
	n, err := p.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 drawer after replace, got %d", n)
	}
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"source_file": "a.md"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Document != "updated" {
		t.Errorf("replace failed: %+v", got)
	}
}
