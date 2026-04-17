package dedup_test

// FakeEmbedder note: the tests below use pkg/embed.FakeEmbedder, which
// produces SHA-256-derived all-positive [0,1] vectors. Identical strings
// give identical vectors (cosine sim == 1.0). Different strings give
// uncorrelated vectors with cosine sim typically ~0.75. As a result, any
// "near-identical" test here uses IDENTICAL content — the fake has no
// locality to exploit. Semantic near-miss coverage lives under
// test/semantic with a real embedder.

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/guerra/go-palace/pkg/dedup"
	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/palace"
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

func makeDrawer(wing, hall, room, src string, chunk int, doc string) palace.Drawer {
	return palace.Drawer{
		ID:         palace.ComputeDrawerID(wing, room, src, chunk),
		Document:   doc,
		Wing:       wing,
		Hall:       hall,
		Room:       room,
		SourceFile: src,
		ChunkIndex: chunk,
		AddedBy:    "test",
		FiledAt:    time.Now(),
	}
}

func TestRun_IdenticalMerged(t *testing.T) {
	p := openTest(t)
	doc := "identical doc content that is long enough to exceed the short threshold"
	var ds []palace.Drawer
	for i := 0; i < 6; i++ {
		ds = append(ds, makeDrawer("w", "knowledge", "r", "a.txt", i, doc))
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	report, err := dedup.Run(p, dedup.DedupOptions{Threshold: 0.95, MinCount: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Dropped != 5 {
		t.Errorf("Dropped = %d, want 5", report.Dropped)
	}
	if report.Kept != 1 {
		t.Errorf("Kept = %d, want 1", report.Kept)
	}
	count, _ := p.Count()
	if count != 1 {
		t.Errorf("palace count after dedup = %d, want 1", count)
	}
}

func TestRun_NearIdenticalMerged(t *testing.T) {
	// Fake embedder has no locality — "near-identical" with fake means
	// identical content (we can't probe semantic distance).
	p := openTest(t)
	doc := "another identical doc batch long enough for dedup threshold checks"
	var ds []palace.Drawer
	for i := 0; i < 4; i++ {
		ds = append(ds, makeDrawer("w", "knowledge", "r", "b.txt", i, doc))
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	report, err := dedup.Run(p, dedup.DedupOptions{Threshold: 0.95, MinCount: 4})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Kept != 1 || report.Dropped != 3 {
		t.Errorf("Kept=%d Dropped=%d, want Kept=1 Dropped=3", report.Kept, report.Dropped)
	}
}

func TestRun_DifferentUntouched(t *testing.T) {
	// Unrelated docs — use Threshold 0.99 so FakeEmbedder's ~0.75
	// random-pair similarity does NOT trigger merges.
	p := openTest(t)
	ds := []palace.Drawer{
		makeDrawer("w", "knowledge", "r", "c.txt", 0, "unique content about Go programming and error handling practices"),
		makeDrawer("w", "knowledge", "r", "c.txt", 1, "entirely different topic: Python data science and pandas workflows"),
		makeDrawer("w", "knowledge", "r", "c.txt", 2, "third subject: JavaScript frontend frameworks and React component design"),
		makeDrawer("w", "knowledge", "r", "c.txt", 3, "fourth: Rust memory safety and ownership model in systems programming"),
		makeDrawer("w", "knowledge", "r", "c.txt", 4, "fifth: PostgreSQL indexing and query optimization strategies and tricks"),
		makeDrawer("w", "knowledge", "r", "c.txt", 5, "sixth: Kubernetes pod scheduling and resource requests semantics review"),
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	report, err := dedup.Run(p, dedup.DedupOptions{Threshold: 0.99, MinCount: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Dropped != 0 {
		t.Errorf("Dropped = %d, want 0 (different docs should stay)", report.Dropped)
	}
	count, _ := p.Count()
	if count != 6 {
		t.Errorf("palace count = %d, want 6", count)
	}
}

func TestRun_CrossHallNotDeduped(t *testing.T) {
	p := openTest(t)
	doc := "identical doc content across halls used to verify partition isolation"
	var ds []palace.Drawer
	for i := 0; i < 5; i++ {
		ds = append(ds, makeDrawer("w", "knowledge", "r", "d.txt", i, doc))
	}
	for i := 0; i < 5; i++ {
		ds = append(ds, makeDrawer("w", "diary", "r", "d.txt", i+100, doc))
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_, err := dedup.Run(p, dedup.DedupOptions{Threshold: 0.95, MinCount: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	count, _ := p.Count()
	// Each hall group dedupes to 1 survivor → total 2.
	if count != 2 {
		t.Errorf("palace count after cross-hall dedup = %d, want 2 (one per hall)", count)
	}
	// Spot-check both halls survived.
	k, _ := p.CountWhere(map[string]string{"hall": "knowledge"})
	d, _ := p.CountWhere(map[string]string{"hall": "diary"})
	if k == 0 || d == 0 {
		t.Errorf("expected both halls to survive; knowledge=%d diary=%d", k, d)
	}
}

func TestRun_DryRun(t *testing.T) {
	p := openTest(t)
	doc := "dry run identical doc content long enough to exceed short threshold"
	var ds []palace.Drawer
	for i := 0; i < 6; i++ {
		ds = append(ds, makeDrawer("w", "knowledge", "r", "dr.txt", i, doc))
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	before, _ := p.Count()
	report, err := dedup.Run(p, dedup.DedupOptions{
		Threshold: 0.95, MinCount: 5, DryRun: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	after, _ := p.Count()
	if after != before {
		t.Errorf("dry run changed palace: before=%d after=%d", before, after)
	}
	if report.Dropped == 0 {
		t.Error("dry run should still report planned drops")
	}
}

func TestRun_MergeMetadata(t *testing.T) {
	p := openTest(t)
	doc := "merge metadata identical doc content long enough for the test threshold"
	// Winner: longer doc (pad with prefix)
	winner := makeDrawer("w", "knowledge", "r", "mm.txt", 0, "WINNER PREFIX "+doc)
	winner.Metadata = map[string]any{"tag": "w"}
	l1 := makeDrawer("w", "knowledge", "r", "mm.txt", 1, doc)
	l1.Metadata = map[string]any{"tag": "L1", "extra": float64(1)}
	l2 := makeDrawer("w", "knowledge", "r", "mm.txt", 2, doc)
	l2.Metadata = map[string]any{"tag": "L2", "extra": float64(2)}
	// Pad to reach MinCount.
	pad1 := makeDrawer("w", "knowledge", "r", "mm.txt", 3, doc)
	pad2 := makeDrawer("w", "knowledge", "r", "mm.txt", 4, doc)
	if err := p.UpsertBatch([]palace.Drawer{winner, l1, l2, pad1, pad2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	report, err := dedup.Run(p, dedup.DedupOptions{
		Threshold: 0.95, MinCount: 5, Merge: true,
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Merged == 0 {
		t.Errorf("expected Merged > 0, got 0 (Dropped=%d)", report.Dropped)
	}
	// Winner must survive and its metadata must contain at least one merged key.
	got, err := p.GetByIDs([]string{winner.ID})
	if err != nil || len(got) != 1 {
		t.Fatalf("winner fetch: err=%v len=%d", err, len(got))
	}
	if got[0].Metadata == nil {
		t.Fatal("winner has nil metadata")
	}
	if _, ok := got[0].Metadata["extra"]; !ok {
		t.Errorf("winner metadata missing merged key 'extra': %v", got[0].Metadata)
	}
}

func TestRun_BelowMinCount(t *testing.T) {
	p := openTest(t)
	doc := "below min count identical doc content long enough to pass short threshold"
	var ds []palace.Drawer
	for i := 0; i < 3; i++ {
		ds = append(ds, makeDrawer("w", "knowledge", "r", "bm.txt", i, doc))
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	report, err := dedup.Run(p, dedup.DedupOptions{Threshold: 0.95, MinCount: 5})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.SourcesChecked != 0 {
		t.Errorf("SourcesChecked = %d, want 0 (group below minCount)", report.SourcesChecked)
	}
	count, _ := p.Count()
	if count != 3 {
		t.Errorf("palace count = %d, want 3 (nothing should be deleted)", count)
	}
}

func TestRun_InvalidThreshold(t *testing.T) {
	p := openTest(t)
	_, err := dedup.Run(p, dedup.DedupOptions{Threshold: 1.5, MinCount: 5})
	if !errors.Is(err, dedup.ErrInvalidThreshold) {
		t.Errorf("err = %v, want ErrInvalidThreshold", err)
	}
}
