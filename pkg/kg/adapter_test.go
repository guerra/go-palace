package kg_test

import (
	"path/filepath"
	"testing"

	"github.com/guerra/go-palace/pkg/kg"
	"github.com/guerra/go-palace/pkg/palace"
)

func TestPalaceAdapter_AddTripleInsertsIntoKG(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "kg.db")
	k, err := kg.Open(dbPath)
	if err != nil {
		t.Fatalf("open kg: %v", err)
	}
	t.Cleanup(func() { _ = k.Close() })

	sink := kg.NewPalaceAdapter(k)
	id, err := sink.AddTriple(palace.TripleRow{
		Subject:      "Alice",
		Predicate:    "works_at",
		Object:       "Acme",
		ValidFrom:    "2026-04-17",
		Confidence:   0.6,
		SourceCloset: "drawer_1",
	})
	if err != nil {
		t.Fatalf("adapter AddTriple: %v", err)
	}
	if id == "" {
		t.Error("AddTriple returned empty id")
	}

	stats, err := k.Stats()
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Triples != 1 {
		t.Errorf("triples = %d; want 1", stats.Triples)
	}
	if stats.Entities != 2 {
		t.Errorf("entities = %d; want 2 (Alice, Acme)", stats.Entities)
	}
}

func TestStatelessEntityDetector_WiresIntoDetect(t *testing.T) {
	det := kg.NewStatelessEntityDetector()
	// Use an email which entity.Detect picks up with high confidence.
	ents := det.DetectEntities("Ping me at alice@example.com for details.")
	if len(ents) == 0 {
		t.Fatal("expected at least one entity from DetectEntities")
	}
	found := false
	for _, e := range ents {
		if e.Type == "email" && e.Name == "alice@example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected email entity; got %+v", ents)
	}
}
