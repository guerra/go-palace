package entity_test

import (
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/entity"
	"github.com/guerra/go-palace/pkg/palace"
)

// newMemRegistry returns a Registry backed by a fresh MemoryStore with no seeds.
func newMemRegistry(t *testing.T) *entity.Registry {
	t.Helper()
	r, err := entity.NewRegistry(entity.RegistryOptions{
		Store:         entity.NewMemoryStore(),
		SeedsDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	return r
}

// openTestPalace returns a freshly-opened palace at a tempdir path, with
// Close registered via t.Cleanup.
func openTestPalace(t *testing.T) *palace.Palace {
	t.Helper()
	p, err := palace.Open(filepath.Join(t.TempDir(), "p.db"),
		embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("palace.Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// openTestRegistry returns a Registry backed by a PalaceStore over a fresh
// palace. Close order: registry first, then palace (LIFO t.Cleanup matches).
func openTestRegistry(t *testing.T, p *palace.Palace) *entity.Registry {
	t.Helper()
	r, err := entity.NewRegistry(entity.RegistryOptions{
		Store:         entity.NewPalaceStore(p),
		SeedsDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewRegistry palace: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestRegistryAddHappyPath(t *testing.T) {
	r := newMemRegistry(t)
	e := entity.Entity{Name: "Riley", Type: entity.TypePerson, Canonical: "Riley"}
	if err := r.Add(e); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Lookup("Riley")
	if !ok {
		t.Fatalf("Lookup after Add: not found")
	}
	if got.Name != "Riley" || got.Type != entity.TypePerson {
		t.Errorf("Lookup result: %+v", got)
	}
}

func TestRegistryAddRejectsDuplicate(t *testing.T) {
	r := newMemRegistry(t)
	e := entity.Entity{Name: "Riley", Type: entity.TypePerson}
	if err := r.Add(e); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	err := r.Add(e)
	if !errors.Is(err, entity.ErrEntityExists) {
		t.Errorf("second Add err: got %v, want ErrEntityExists", err)
	}
}

func TestRegistryMergeUpserts(t *testing.T) {
	r := newMemRegistry(t)
	e := entity.Entity{Name: "Foo", Type: entity.TypeProject, Canonical: "Foo"}
	if err := r.Merge(e); err != nil {
		t.Fatalf("Merge 1: %v", err)
	}
	e.Canonical = "Foo v2"
	if err := r.Merge(e); err != nil {
		t.Fatalf("Merge 2: %v", err)
	}
	got, _ := r.Lookup("Foo")
	if got.Canonical != "Foo v2" {
		t.Errorf("Merge did not upsert canonical: %q", got.Canonical)
	}
}

func TestRegistryLookupCaseInsensitive(t *testing.T) {
	r := newMemRegistry(t)
	_ = r.Add(entity.Entity{Name: "Riley", Type: entity.TypePerson})
	if _, ok := r.Lookup("riley"); !ok {
		t.Error("lowercase lookup failed")
	}
	if _, ok := r.Lookup("RILEY"); !ok {
		t.Error("uppercase lookup failed")
	}
}

func TestRegistryLookupByAlias(t *testing.T) {
	r := newMemRegistry(t)
	e := entity.Entity{
		Name:    "Maxwell",
		Type:    entity.TypePerson,
		Aliases: []string{"Max"},
	}
	if err := r.Add(e); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, ok := r.Lookup("Max")
	if !ok {
		t.Fatalf("alias lookup failed")
	}
	if got.Name != "Maxwell" {
		t.Errorf("alias returned %q, want Maxwell", got.Name)
	}
}

func TestRegistryLookupMissing(t *testing.T) {
	r := newMemRegistry(t)
	if _, ok := r.Lookup("Zarquon"); ok {
		t.Error("missing lookup returned ok=true")
	}
}

func TestRegistryListByType(t *testing.T) {
	r := newMemRegistry(t)
	_ = r.Add(entity.Entity{Name: "Alice", Type: entity.TypePerson})
	_ = r.Add(entity.Entity{Name: "Bob", Type: entity.TypePerson})
	_ = r.Add(entity.Entity{Name: "proj1", Type: entity.TypeProject})
	if n := len(r.ListByType(entity.TypePerson)); n != 2 {
		t.Errorf("persons: got %d want 2", n)
	}
	if n := len(r.ListByType(entity.TypeProject)); n != 1 {
		t.Errorf("projects: got %d want 1", n)
	}
	if n := len(r.ListByType(entity.TypeTool)); n != 0 {
		t.Errorf("tools (seeds disabled): got %d want 0", n)
	}
}

func TestRegistrySeedsEnabled(t *testing.T) {
	r, err := entity.NewRegistry(entity.RegistryOptions{}) // zero-value: seeds on, mem store
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	tools := r.ListByType(entity.TypeTool)
	if len(tools) < 30 {
		t.Errorf("expected ~40 tool seeds, got %d", len(tools))
	}
}

func TestRegistrySeedsDisabled(t *testing.T) {
	r := newMemRegistry(t)
	if n := len(r.ListByType(entity.TypeTool)); n != 0 {
		t.Errorf("seeds disabled: expected 0 tools, got %d", n)
	}
}

func TestRegistryMemoryStoreRoundtrip(t *testing.T) {
	store := entity.NewMemoryStore()
	r, err := entity.NewRegistry(entity.RegistryOptions{Store: store, SeedsDisabled: true})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	names := []string{"Alpha", "Bravo", "Charlie"}
	for _, n := range names {
		if err := r.Add(entity.Entity{Name: n, Type: entity.TypePerson}); err != nil {
			t.Fatalf("Add %s: %v", n, err)
		}
	}
	// Re-open on the same store.
	r2, err := entity.NewRegistry(entity.RegistryOptions{Store: store, SeedsDisabled: true})
	if err != nil {
		t.Fatalf("NewRegistry 2: %v", err)
	}
	for _, n := range names {
		if _, ok := r2.Lookup(n); !ok {
			t.Errorf("lost %s across registry reopen", n)
		}
	}
}

func TestRegistryPalaceStoreRoundtrip(t *testing.T) {
	p := openTestPalace(t)
	r1, err := entity.NewRegistry(entity.RegistryOptions{
		Store: entity.NewPalaceStore(p), SeedsDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewRegistry 1: %v", err)
	}
	if err := r1.Add(entity.Entity{
		Name: "ChromaDB", Type: entity.TypeProject, Canonical: "ChromaDB",
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := r1.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}

	r2, err := entity.NewRegistry(entity.RegistryOptions{
		Store: entity.NewPalaceStore(p), SeedsDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewRegistry 2: %v", err)
	}
	got, ok := r2.Lookup("ChromaDB")
	if !ok {
		t.Fatalf("ChromaDB not persisted")
	}
	if got.Type != entity.TypeProject || got.Canonical != "ChromaDB" {
		t.Errorf("round-trip mismatch: %+v", got)
	}
}

func TestRegistryConcurrentAdd(t *testing.T) {
	r := newMemRegistry(t)
	const (
		goroutines = 10
		perGR      = 10
	)
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for g := 0; g < goroutines; g++ {
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perGR; i++ {
				name := fmt.Sprintf("g%d-e%d", g, i)
				if err := r.Add(entity.Entity{Name: name, Type: entity.TypePerson}); err != nil {
					t.Errorf("Add %s: %v", name, err)
				}
			}
		}(g)
	}
	wg.Wait()
	got := r.List()
	if len(got) != goroutines*perGR {
		t.Errorf("final count: got %d want %d", len(got), goroutines*perGR)
	}
}

func TestRegistryMemoryStoreDelete(t *testing.T) {
	ms := entity.NewMemoryStore()
	r, err := entity.NewRegistry(entity.RegistryOptions{Store: ms, SeedsDisabled: true})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	_ = r.Add(entity.Entity{Name: "ToDelete", Type: entity.TypePerson})
	// Deletion of persisted rows happens via the store directly; Registry has
	// no Delete of its own per the plan's API surface. Confirm store.Delete
	// is idempotent:
	if err := ms.Delete("Missing"); err != nil {
		t.Errorf("Delete missing err: %v", err)
	}
	if err := ms.Delete("ToDelete"); err != nil {
		t.Errorf("Delete existing: %v", err)
	}
}

func TestRegistryEmptyNameAdd(t *testing.T) {
	r := newMemRegistry(t)
	if err := r.Add(entity.Entity{Name: "", Type: entity.TypePerson}); err == nil {
		t.Error("expected error on empty-name Add")
	}
}

// TestRegistryMergePreservesAccumulation verifies Merge preserves FirstSeen
// and increments OccurrenceCount across repeat observations. Regression
// guard for the gp-3 code-review HIGH finding.
func TestRegistryMergePreservesAccumulation(t *testing.T) {
	p := openTestPalace(t)
	store := entity.NewPalaceStore(p)
	r, err := entity.NewRegistry(entity.RegistryOptions{Store: store, SeedsDisabled: true})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}

	e := entity.Entity{Name: "Foo", Type: entity.TypeProject, Canonical: "Foo"}
	if err := r.Merge(e); err != nil {
		t.Fatalf("Merge 1: %v", err)
	}
	rows1, _ := p.EntityList()
	if len(rows1) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows1))
	}
	first := rows1[0].FirstSeen
	if rows1[0].OccurrenceCount != 1 {
		t.Errorf("OccurrenceCount after first Merge: got %d want 1", rows1[0].OccurrenceCount)
	}

	// Second merge: FirstSeen MUST NOT change; OccurrenceCount MUST become 2.
	time.Sleep(2 * time.Millisecond) // ensure now > first
	if err := r.Merge(e); err != nil {
		t.Fatalf("Merge 2: %v", err)
	}
	rows2, _ := p.EntityList()
	if len(rows2) != 1 {
		t.Fatalf("want 1 row, got %d", len(rows2))
	}
	if !rows2[0].FirstSeen.Equal(first) {
		t.Errorf("FirstSeen changed across Merge: first=%v second=%v",
			first, rows2[0].FirstSeen)
	}
	if rows2[0].OccurrenceCount != 2 {
		t.Errorf("OccurrenceCount after second Merge: got %d want 2",
			rows2[0].OccurrenceCount)
	}
	if !rows2[0].LastSeen.After(first) {
		t.Errorf("LastSeen did not advance: first=%v last=%v",
			first, rows2[0].LastSeen)
	}
}

// TestRegistryCloseDoesNotFlushSeeds verifies seeds remain in-memory only —
// the entities table must be empty after NewRegistry(seeds-on)+Close on a
// PalaceStore-backed registry with no user writes. Regression guard for the
// gp-3 code-review MEDIUM finding (seed flush on Close).
func TestRegistryCloseDoesNotFlushSeeds(t *testing.T) {
	p := openTestPalace(t)
	r, err := entity.NewRegistry(entity.RegistryOptions{
		Store: entity.NewPalaceStore(p),
		// SeedsDisabled left false (zero value) — seeds ON.
	})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	// Seeds present in in-memory view.
	if n := len(r.ListByType(entity.TypeTool)); n < 30 {
		t.Fatalf("expected seeded tools in memory, got %d", n)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// But the entities table itself must still be empty.
	rows, err := p.EntityList()
	if err != nil {
		t.Fatalf("EntityList: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("Close leaked seeds to store: got %d rows, want 0", len(rows))
	}
}

// TestRegistryCloseIdempotent verifies repeat Close calls are safe and
// cheap. Regression guard for the gp-3 code-review MEDIUM finding.
func TestRegistryCloseIdempotent(t *testing.T) {
	r := newMemRegistry(t)
	if err := r.Close(); err != nil {
		t.Fatalf("Close 1: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close 2: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close 3: %v", err)
	}
}

// TestRegistryTolerantOfCorruptRow verifies a malformed AliasesJSON row
// does not abort NewRegistry — it is skipped with a warning. Regression
// guard for the gp-3 code-review MEDIUM finding (NewRegistry corrupt-row DoS).
func TestRegistryTolerantOfCorruptRow(t *testing.T) {
	p := openTestPalace(t)
	// Inject a good row and a row with corrupt AliasesJSON. Two Upsert calls
	// direct to Palace (bypass Registry so the corruption survives).
	good := palace.EntityRow{
		Name:            "Good",
		Type:            "person",
		Canonical:       "Good",
		AliasesJSON:     "[]",
		FirstSeen:       time.Now().UTC(),
		LastSeen:        time.Now().UTC(),
		OccurrenceCount: 1,
	}
	bad := palace.EntityRow{
		Name:            "Bad",
		Type:            "person",
		Canonical:       "Bad",
		AliasesJSON:     `{not valid json`,
		FirstSeen:       time.Now().UTC(),
		LastSeen:        time.Now().UTC(),
		OccurrenceCount: 1,
	}
	if err := p.EntityUpsert(good); err != nil {
		t.Fatalf("Upsert good: %v", err)
	}
	if err := p.EntityUpsert(bad); err != nil {
		t.Fatalf("Upsert bad: %v", err)
	}

	// NewRegistry must SUCCEED (bad row skipped with warning).
	r, err := entity.NewRegistry(entity.RegistryOptions{
		Store: entity.NewPalaceStore(p), SeedsDisabled: true,
	})
	if err != nil {
		t.Fatalf("NewRegistry aborted on corrupt row: %v", err)
	}
	if _, ok := r.Lookup("Good"); !ok {
		t.Error("Good not reachable after skip")
	}
	if _, ok := r.Lookup("Bad"); ok {
		t.Error("Bad should have been skipped")
	}
}
