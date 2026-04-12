package kg_test

import (
	"path/filepath"
	"strings"
	"testing"

	"go-palace/internal/kg"
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
	}
	for _, tt := range tests {
		got := kg.EntityID(tt.input)
		if got != tt.want {
			t.Errorf("EntityID(%q): got %q, want %q", tt.input, got, tt.want)
		}
	}
}
