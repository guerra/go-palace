package kg_test

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/guerra/go-palace/pkg/kg"

	_ "github.com/mattn/go-sqlite3"
)

func openTestKG(t *testing.T) *kg.KG {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test_kg.sqlite3")
	k, err := kg.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = k.Close() })
	return k
}

func TestOpenClose(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_kg.sqlite3")
	k, err := kg.Open(dbPath)
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	if err := k.Close(); err != nil {
		t.Fatalf("close1: %v", err)
	}
	// Reopen: schema should survive
	k2, err := kg.Open(dbPath)
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	defer func() { _ = k2.Close() }()
	stats, err := k2.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Entities != 0 {
		t.Errorf("entities: got %d, want 0", stats.Entities)
	}
}

func TestAddTriple(t *testing.T) {
	k := openTestKG(t)
	id, err := k.AddTriple(kg.Triple{
		Subject:   "Max",
		Predicate: "child_of",
		Object:    "Alice",
		ValidFrom: "2015-04-01",
	})
	if err != nil {
		t.Fatalf("AddTriple: %v", err)
	}
	// Triple ID format
	if !strings.HasPrefix(id, "t_max_child_of_alice_") {
		t.Errorf("unexpected triple ID: %s", id)
	}
	// Entities auto-created
	stats, err := k.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Entities != 2 {
		t.Errorf("entities: got %d, want 2", stats.Entities)
	}
	if stats.Triples != 1 {
		t.Errorf("triples: got %d, want 1", stats.Triples)
	}
}

func TestAddTripleIdempotent(t *testing.T) {
	k := openTestKG(t)
	id1, err := k.AddTriple(kg.Triple{Subject: "Max", Predicate: "loves", Object: "chess"})
	if err != nil {
		t.Fatalf("add1: %v", err)
	}
	id2, err := k.AddTriple(kg.Triple{Subject: "Max", Predicate: "loves", Object: "chess"})
	if err != nil {
		t.Fatalf("add2: %v", err)
	}
	if id1 != id2 {
		t.Errorf("idempotent: got different IDs %q vs %q", id1, id2)
	}
}

func TestQueryEntity(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "child_of", Object: "Alice"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "loves", Object: "chess"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Bob", Predicate: "friend_of", Object: "Max"})

	// Outgoing
	facts, err := k.QueryEntity("Max", kg.QueryOpts{Direction: "outgoing"})
	if err != nil {
		t.Fatalf("query outgoing: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("outgoing: got %d, want 2", len(facts))
	}

	// Incoming
	facts, err = k.QueryEntity("Max", kg.QueryOpts{Direction: "incoming"})
	if err != nil {
		t.Fatalf("query incoming: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("incoming: got %d, want 1", len(facts))
	}

	// Both
	facts, err = k.QueryEntity("Max", kg.QueryOpts{Direction: "both"})
	if err != nil {
		t.Fatalf("query both: %v", err)
	}
	if len(facts) != 3 {
		t.Errorf("both: got %d, want 3", len(facts))
	}
}

func TestQueryEntityAsOf(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "does", Object: "swimming", ValidFrom: "2025-01-01"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "does", Object: "chess", ValidFrom: "2025-06-01"})

	// Query as of March — only swimming
	facts, err := k.QueryEntity("Max", kg.QueryOpts{AsOf: "2025-03-15"})
	if err != nil {
		t.Fatalf("query asOf: %v", err)
	}
	if len(facts) != 1 {
		t.Errorf("asOf March: got %d, want 1", len(facts))
	}
	if len(facts) > 0 && facts[0].Object != "swimming" {
		t.Errorf("asOf March: got %q, want swimming", facts[0].Object)
	}

	// Query as of July — both
	facts, err = k.QueryEntity("Max", kg.QueryOpts{AsOf: "2025-07-01"})
	if err != nil {
		t.Fatalf("query asOf July: %v", err)
	}
	if len(facts) != 2 {
		t.Errorf("asOf July: got %d, want 2", len(facts))
	}
}

func TestInvalidate(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "does", Object: "swimming"})

	err := k.Invalidate("Max", "does", "swimming", "2026-02-15")
	if err != nil {
		t.Fatalf("invalidate: %v", err)
	}

	facts, _ := k.QueryEntity("Max", kg.QueryOpts{})
	for _, f := range facts {
		if f.Object == "swimming" && f.Current {
			t.Error("swimming should no longer be current")
		}
	}

	stats, _ := k.Stats()
	if stats.ExpiredFacts != 1 {
		t.Errorf("expired: got %d, want 1", stats.ExpiredFacts)
	}
}

func TestTimeline(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "born", Object: "world", ValidFrom: "2015-04-01"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "does", Object: "swimming", ValidFrom: "2025-01-01"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "loves", Object: "chess", ValidFrom: "2025-06-01"})

	facts, err := k.Timeline("Max")
	if err != nil {
		t.Fatalf("timeline: %v", err)
	}
	if len(facts) != 3 {
		t.Errorf("timeline: got %d, want 3", len(facts))
	}
	// Should be chronological
	if len(facts) >= 2 && facts[0].ValidFrom > facts[1].ValidFrom {
		t.Error("timeline not chronological")
	}

	// Global timeline
	facts, err = k.Timeline("")
	if err != nil {
		t.Fatalf("global timeline: %v", err)
	}
	if len(facts) != 3 {
		t.Errorf("global: got %d, want 3", len(facts))
	}
}

func TestStats(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "child_of", Object: "Alice"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Max", Predicate: "loves", Object: "chess"})
	_ = k.Invalidate("Max", "loves", "chess", "2026-01-01")

	stats, err := k.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Entities != 3 { // Max, Alice, chess
		t.Errorf("entities: got %d, want 3", stats.Entities)
	}
	if stats.Triples != 2 {
		t.Errorf("triples: got %d, want 2", stats.Triples)
	}
	if stats.CurrentFacts != 1 {
		t.Errorf("current: got %d, want 1", stats.CurrentFacts)
	}
	if stats.ExpiredFacts != 1 {
		t.Errorf("expired: got %d, want 1", stats.ExpiredFacts)
	}
	if len(stats.RelationshipTypes) != 2 {
		t.Errorf("types: got %d, want 2", len(stats.RelationshipTypes))
	}
}

func TestEntityIDNormalization(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"Max Power", "max_power"},
		{"O'Brien", "obrien"},
		{"alice", "alice"},
		{"John Smith Jr", "john_smith_jr"},
		{"Dr. Chen", "dr._chen"},
	}
	for _, tt := range tests {
		got := kg.EntityID(tt.input)
		if got != tt.want {
			t.Errorf("EntityID(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}

// --- seedTestKG helper: mirrors Python conftest seeded_kg ---

func seedTestKG(t *testing.T) *kg.KG {
	t.Helper()
	k := openTestKG(t)
	triples := []kg.Triple{
		{Subject: "Alice", Predicate: "parent_of", Object: "Max", ValidFrom: "2015-04-01"},
		{Subject: "Alice", Predicate: "works_at", Object: "Acme Corp", ValidFrom: "2020-01-01", ValidTo: "2024-12-31"},
		{Subject: "Alice", Predicate: "works_at", Object: "NewCo", ValidFrom: "2025-01-01"},
		{Subject: "Max", Predicate: "does", Object: "swimming", ValidFrom: "2025-01-01"},
		{Subject: "Max", Predicate: "does", Object: "chess", ValidFrom: "2025-06-01"},
	}
	for _, tr := range triples {
		if _, err := k.AddTriple(tr); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	return k
}

// --- Entity operations ---

func TestAddEntity(t *testing.T) {
	k := openTestKG(t)
	eid, err := k.AddEntity("Alice", "person", nil)
	if err != nil {
		t.Fatalf("AddEntity: %v", err)
	}
	if eid != "alice" {
		t.Errorf("got %q, want alice", eid)
	}
}

func TestAddEntityNormalizesID(t *testing.T) {
	k := openTestKG(t)
	eid, err := k.AddEntity("Dr. Chen", "person", nil)
	if err != nil {
		t.Fatalf("AddEntity: %v", err)
	}
	if eid != "dr._chen" {
		t.Errorf("got %q, want dr._chen", eid)
	}
}

func TestAddEntityUpsert(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddEntity("Alice", "person", nil)
	_, _ = k.AddEntity("Alice", "engineer", nil)
	stats, err := k.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entities != 1 {
		t.Errorf("entities = %d, want 1 (upsert)", stats.Entities)
	}
}

// --- Triple edge cases ---

func TestInvalidatedTripleAllowsReAdd(t *testing.T) {
	k := openTestKG(t)
	id1, _ := k.AddTriple(kg.Triple{Subject: "Alice", Predicate: "works_at", Object: "Acme"})
	_ = k.Invalidate("Alice", "works_at", "Acme", "2025-01-01")
	id2, _ := k.AddTriple(kg.Triple{Subject: "Alice", Predicate: "works_at", Object: "Acme"})
	if id1 == id2 {
		t.Error("expected different ID after invalidation and re-add")
	}
}

// --- Query tests with seeded KG ---

func TestQueryOutgoing(t *testing.T) {
	k := seedTestKG(t)
	facts, err := k.QueryEntity("Alice", kg.QueryOpts{Direction: "outgoing"})
	if err != nil {
		t.Fatal(err)
	}
	predicates := map[string]bool{}
	for _, f := range facts {
		predicates[f.Predicate] = true
	}
	if !predicates["parent_of"] {
		t.Error("missing parent_of in outgoing")
	}
	if !predicates["works_at"] {
		t.Error("missing works_at in outgoing")
	}
}

func TestQueryIncoming(t *testing.T) {
	k := seedTestKG(t)
	facts, err := k.QueryEntity("Max", kg.QueryOpts{Direction: "incoming"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range facts {
		if f.Subject == "Alice" && f.Predicate == "parent_of" {
			found = true
		}
	}
	if !found {
		t.Error("expected Alice parent_of Max in incoming results")
	}
}

func TestQueryBothDirections(t *testing.T) {
	k := seedTestKG(t)
	facts, err := k.QueryEntity("Max", kg.QueryOpts{Direction: "both"})
	if err != nil {
		t.Fatal(err)
	}
	directions := map[string]bool{}
	for _, f := range facts {
		directions[f.Direction] = true
	}
	if !directions["outgoing"] {
		t.Error("missing outgoing direction")
	}
	if !directions["incoming"] {
		t.Error("missing incoming direction")
	}
}

func TestQueryAsOfFiltersExpired(t *testing.T) {
	k := seedTestKG(t)
	facts, err := k.QueryEntity("Alice", kg.QueryOpts{AsOf: "2023-06-01", Direction: "outgoing"})
	if err != nil {
		t.Fatal(err)
	}
	var employers []string
	for _, f := range facts {
		if f.Predicate == "works_at" {
			employers = append(employers, f.Object)
		}
	}
	found := false
	for _, e := range employers {
		if e == "Acme Corp" {
			found = true
		}
		if e == "NewCo" {
			t.Error("NewCo should not be visible as of 2023-06-01")
		}
	}
	if !found {
		t.Error("Acme Corp should be visible as of 2023-06-01")
	}
}

func TestQueryAsOfShowsCurrent(t *testing.T) {
	k := seedTestKG(t)
	facts, err := k.QueryEntity("Alice", kg.QueryOpts{AsOf: "2025-06-01", Direction: "outgoing"})
	if err != nil {
		t.Fatal(err)
	}
	var employers []string
	for _, f := range facts {
		if f.Predicate == "works_at" {
			employers = append(employers, f.Object)
		}
	}
	foundNewCo := false
	for _, e := range employers {
		if e == "NewCo" {
			foundNewCo = true
		}
		if e == "Acme Corp" {
			t.Error("Acme Corp should not be visible as of 2025-06-01 (expired)")
		}
	}
	if !foundNewCo {
		t.Error("NewCo should be visible as of 2025-06-01")
	}
}

func TestQueryRelationship(t *testing.T) {
	k := seedTestKG(t)
	results, err := k.QueryRelationship("does", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 'does' triples, got %d", len(results))
	}
}

func TestQueryRelationshipWithAsOf(t *testing.T) {
	k := openTestKG(t)
	_, _ = k.AddTriple(kg.Triple{Subject: "Alice", Predicate: "works_at", Object: "Acme", ValidFrom: "2020-01-01", ValidTo: "2024-12-31"})
	_, _ = k.AddTriple(kg.Triple{Subject: "Alice", Predicate: "works_at", Object: "NewCo", ValidFrom: "2025-01-01"})

	results, err := k.QueryRelationship("works_at", "2023-06-01")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range results {
		if r.Object == "NewCo" {
			t.Error("NewCo should not appear for as_of=2023-06-01")
		}
	}
	found := false
	for _, r := range results {
		if r.Object == "Acme" {
			found = true
		}
	}
	if !found {
		t.Error("Acme should appear for as_of=2023-06-01")
	}
}

// --- Invalidation ---

func TestInvalidateSetsValidTo(t *testing.T) {
	k := seedTestKG(t)
	_ = k.Invalidate("Max", "does", "chess", "2026-01-01")
	facts, _ := k.QueryEntity("Max", kg.QueryOpts{Direction: "outgoing"})
	for _, f := range facts {
		if f.Object == "chess" {
			if f.ValidTo != "2026-01-01" {
				t.Errorf("valid_to = %q, want 2026-01-01", f.ValidTo)
			}
			if f.Current {
				t.Error("chess should not be current")
			}
		}
	}
}

// --- Timeline ---

func TestTimelineAll(t *testing.T) {
	k := seedTestKG(t)
	tl, err := k.Timeline("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) < 4 {
		t.Errorf("global timeline = %d, want >= 4", len(tl))
	}
}

func TestTimelineEntity(t *testing.T) {
	k := seedTestKG(t)
	tl, err := k.Timeline("Max")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range tl {
		if f.Subject != "Max" && f.Object != "Max" {
			t.Errorf("timeline entry not related to Max: %+v", f)
		}
	}
}

func TestTimelineGlobalHasLimit(t *testing.T) {
	k := openTestKG(t)
	for i := 0; i < 105; i++ {
		_, _ = k.AddTriple(kg.Triple{
			Subject: fmt.Sprintf("entity_%d", i), Predicate: "relates_to", Object: fmt.Sprintf("entity_%d", i+1),
		})
	}
	tl, err := k.Timeline("")
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 100 {
		t.Errorf("global timeline = %d, want 100 (LIMIT)", len(tl))
	}
}

func TestTimelineEntityHasLimit(t *testing.T) {
	k := openTestKG(t)
	for i := 0; i < 105; i++ {
		_, _ = k.AddTriple(kg.Triple{
			Subject: "hub", Predicate: "connects_to", Object: fmt.Sprintf("spoke_%d", i),
			ValidFrom: fmt.Sprintf("2025-01-%02d", (i%28)+1),
		})
	}
	tl, err := k.Timeline("hub")
	if err != nil {
		t.Fatal(err)
	}
	if len(tl) != 100 {
		t.Errorf("entity timeline = %d, want 100 (LIMIT)", len(tl))
	}
}

// --- WAL mode ---

func TestWALModeEnabled(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "wal_test.sqlite3")
	k, err := kg.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = k.Close() }()

	// We can't directly access the db, so we open a fresh connection to check
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want wal", mode)
	}
}

// --- Stats ---

func TestStatsEmpty(t *testing.T) {
	k := openTestKG(t)
	stats, err := k.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entities != 0 {
		t.Errorf("entities = %d, want 0", stats.Entities)
	}
	if stats.Triples != 0 {
		t.Errorf("triples = %d, want 0", stats.Triples)
	}
}

func TestStatsSeeded(t *testing.T) {
	k := seedTestKG(t)
	stats, err := k.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.Entities < 4 {
		t.Errorf("entities = %d, want >= 4", stats.Entities)
	}
	if stats.Triples != 5 {
		t.Errorf("triples = %d, want 5", stats.Triples)
	}
	if stats.CurrentFacts != 4 {
		t.Errorf("current = %d, want 4", stats.CurrentFacts)
	}
	if stats.ExpiredFacts != 1 {
		t.Errorf("expired = %d, want 1", stats.ExpiredFacts)
	}
}
