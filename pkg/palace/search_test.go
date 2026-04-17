package palace_test

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/palace"
)

func TestQueryBumpsLastAccessedWhenEnabled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	opts := palace.DefaultPalaceOptions()
	opts.TrackLastAccessed = true
	p, err := palace.OpenWithOptions(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	drawers := []palace.Drawer{
		makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "alpha"),
		makeDrawer("w", halls.HallKnowledge, "r", "b.md", 0, "beta"),
		makeDrawer("w", halls.HallKnowledge, "r", "c.md", 0, "gamma"),
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	before := time.Now().UTC()
	res, err := p.Query("alpha", palace.QueryOptions{NResults: 2})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	returnedIDs := map[string]bool{}
	for _, r := range res {
		returnedIDs[r.Drawer.ID] = true
	}

	all, err := p.Get(palace.GetOptions{Limit: 10})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	var bumped, skipped int
	for _, d := range all {
		raw, has := d.Metadata["last_accessed"].(string)
		if returnedIDs[d.ID] {
			if !has {
				t.Errorf("returned drawer %s missing last_accessed", d.ID)
				continue
			}
			ts, err := time.Parse(time.RFC3339Nano, raw)
			if err != nil {
				t.Errorf("parse last_accessed %q: %v", raw, err)
				continue
			}
			if ts.Before(before.Add(-time.Second)) {
				t.Errorf("drawer %s last_accessed %v before query start %v", d.ID, ts, before)
			}
			bumped++
		} else {
			if has {
				t.Errorf("non-returned drawer %s unexpectedly has last_accessed = %q", d.ID, raw)
			}
			skipped++
		}
	}
	if bumped != 2 || skipped != 1 {
		t.Errorf("bumped=%d skipped=%d; want 2 / 1", bumped, skipped)
	}
}

// fakeTripleSink is a minimal palace.TripleSink for OpenWithOptions
// validation tests. Never exercised — the Open call must fail first.
type fakeTripleSink struct{}

func (fakeTripleSink) AddTriple(r palace.TripleRow) (string, error) { return "", nil }

// fakeEntityDetector is a minimal palace.EntityDetector for the same.
type fakeEntityDetector struct{}

func (fakeEntityDetector) DetectEntities(s string) []palace.EntityMatch { return nil }

func TestOpenWithOptions_AutoExtractKG_MissingKG(t *testing.T) {
	opts := palace.DefaultPalaceOptions()
	opts.AutoExtractKG = true
	opts.EntityRegistry = fakeEntityDetector{}
	opts.AutoExtractFn = func(palace.Drawer, []palace.EntityMatch) []palace.TripleRow { return nil }
	path := filepath.Join(t.TempDir(), "p.db")
	_, err := palace.OpenWithOptions(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err == nil {
		t.Fatal("expected ErrInvalidOptions when KG is nil, got nil")
	}
	if !errors.Is(err, palace.ErrInvalidOptions) {
		t.Errorf("err = %v; want ErrInvalidOptions", err)
	}
}

func TestOpenWithOptions_AutoExtractKG_MissingRegistry(t *testing.T) {
	opts := palace.DefaultPalaceOptions()
	opts.AutoExtractKG = true
	opts.KG = fakeTripleSink{}
	opts.AutoExtractFn = func(palace.Drawer, []palace.EntityMatch) []palace.TripleRow { return nil }
	path := filepath.Join(t.TempDir(), "p.db")
	_, err := palace.OpenWithOptions(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err == nil {
		t.Fatal("expected ErrInvalidOptions when EntityRegistry is nil, got nil")
	}
	if !errors.Is(err, palace.ErrInvalidOptions) {
		t.Errorf("err = %v; want ErrInvalidOptions", err)
	}
}

func TestOpenWithOptions_AutoExtractKG_MissingFn(t *testing.T) {
	opts := palace.DefaultPalaceOptions()
	opts.AutoExtractKG = true
	opts.KG = fakeTripleSink{}
	opts.EntityRegistry = fakeEntityDetector{}
	path := filepath.Join(t.TempDir(), "p.db")
	_, err := palace.OpenWithOptions(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err == nil {
		t.Fatal("expected ErrInvalidOptions when AutoExtractFn is nil, got nil")
	}
	if !errors.Is(err, palace.ErrInvalidOptions) {
		t.Errorf("err = %v; want ErrInvalidOptions", err)
	}
}

func TestOpenWithOptions_AutoExtractKG_AllSet(t *testing.T) {
	opts := palace.DefaultPalaceOptions()
	opts.AutoExtractKG = true
	opts.KG = fakeTripleSink{}
	opts.EntityRegistry = fakeEntityDetector{}
	opts.AutoExtractFn = func(palace.Drawer, []palace.EntityMatch) []palace.TripleRow { return nil }
	path := filepath.Join(t.TempDir(), "p.db")
	p, err := palace.OpenWithOptions(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err != nil {
		t.Fatalf("valid AutoExtractKG config should Open: %v", err)
	}
	_ = p.Close()
}

func TestQueryDoesNotBumpByDefault(t *testing.T) {
	p := openTest(t)
	drawers := []palace.Drawer{
		makeDrawer("w", halls.HallKnowledge, "r", "a.md", 0, "alpha"),
		makeDrawer("w", halls.HallKnowledge, "r", "b.md", 0, "beta"),
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if _, err := p.Query("alpha", palace.QueryOptions{NResults: 2}); err != nil {
		t.Fatalf("query: %v", err)
	}
	all, err := p.Get(palace.GetOptions{Limit: 10})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	for _, d := range all {
		if _, has := d.Metadata["last_accessed"]; has {
			t.Errorf("drawer %s got last_accessed with TrackLastAccessed=false", d.ID)
		}
	}
}
