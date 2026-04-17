package kg_test

import (
	"reflect"
	"testing"
	"time"

	"github.com/guerra/go-palace/pkg/kg"
	"github.com/guerra/go-palace/pkg/palace"
)

// makeDrawer builds a minimal drawer for extract tests.
func makeDrawer(id, doc string) palace.Drawer {
	return palace.Drawer{
		ID:       id,
		Document: doc,
		FiledAt:  time.Date(2026, 4, 17, 12, 0, 0, 0, time.UTC),
	}
}

// mkEnts is a terse constructor — each arg is name|type|offset.
// Canonical defaults to Name when empty.
func mkEnts(triples ...[3]any) []palace.EntityMatch {
	out := make([]palace.EntityMatch, 0, len(triples))
	for _, t := range triples {
		name, _ := t[0].(string)
		typ, _ := t[1].(string)
		off, _ := t[2].(int)
		out = append(out, palace.EntityMatch{
			Name:      name,
			Type:      typ,
			Canonical: name,
			Offset:    off,
		})
	}
	return out
}

// TestAutoExtract_VerbCoverage exercises each of the 6 verbs with a positive
// (hit) and a cross-sentence negative (miss) case. The positive doc is
// constructed so entity offsets line up against the surface form.
func TestAutoExtract_VerbCoverage(t *testing.T) {
	cases := []struct {
		name      string
		doc       string
		ents      []palace.EntityMatch
		wantSPO   [3]string // subject, predicate, object — "" means empty output
		predicate string
	}{
		{
			name:      "works_at_hit",
			doc:       "Alice works at Acme today.",
			ents:      mkEnts([3]any{"Alice", "person", 0}, [3]any{"Acme", "project", 15}),
			wantSPO:   [3]string{"Alice", "works_at", "Acme"},
			predicate: "works_at",
		},
		{
			name:      "works_at_cross_sentence",
			doc:       "Alice sat quietly. " + pad(45) + "works at Acme today.",
			ents:      mkEnts([3]any{"Alice", "person", 0}, [3]any{"Acme", "project", 19 + 45 + 9}),
			wantSPO:   [3]string{"", "", ""},
			predicate: "works_at",
		},
		{
			name:      "lives_in_hit",
			doc:       "Bob lives in Berlin now.",
			ents:      mkEnts([3]any{"Bob", "person", 0}, [3]any{"Berlin", "place", 13}),
			wantSPO:   [3]string{"Bob", "lives_in", "Berlin"},
			predicate: "lives_in",
		},
		{
			name:      "lives_in_miss_no_object",
			doc:       "Bob lives in somewhere unknown today.",
			ents:      mkEnts([3]any{"Bob", "person", 0}),
			wantSPO:   [3]string{"", "", ""},
			predicate: "lives_in",
		},
		{
			name:      "uses_hit",
			doc:       "Carol uses Python daily.",
			ents:      mkEnts([3]any{"Carol", "person", 0}, [3]any{"Python", "tool", 11}),
			wantSPO:   [3]string{"Carol", "uses", "Python"},
			predicate: "uses",
		},
		{
			name:      "uses_miss_first_person",
			doc:       "I uses Python daily.",
			ents:      mkEnts([3]any{"I", "person", 0}, [3]any{"Python", "tool", 7}),
			wantSPO:   [3]string{"", "", ""},
			predicate: "uses",
		},
		{
			name:      "prefers_hit",
			doc:       "Dan prefers Emacs always.",
			ents:      mkEnts([3]any{"Dan", "person", 0}, [3]any{"Emacs", "tool", 12}),
			wantSPO:   [3]string{"Dan", "prefers", "Emacs"},
			predicate: "prefers",
		},
		{
			name:      "prefers_miss_wrong_object_type",
			doc:       "Dan prefers Berlin always.",
			ents:      mkEnts([3]any{"Dan", "person", 0}, [3]any{"Berlin", "place", 12}),
			wantSPO:   [3]string{"", "", ""},
			predicate: "prefers",
		},
		{
			name:      "started_hit",
			doc:       "Eve started MemPalace yesterday.",
			ents:      mkEnts([3]any{"Eve", "person", 0}, [3]any{"MemPalace", "project", 12}),
			wantSPO:   [3]string{"Eve", "started", "MemPalace"},
			predicate: "started",
		},
		{
			name:      "started_miss_no_subject",
			doc:       pad(60) + "started MemPalace yesterday.",
			ents:      mkEnts([3]any{"MemPalace", "project", 68}),
			wantSPO:   [3]string{"", "", ""},
			predicate: "started",
		},
		{
			name:      "finished_hit",
			doc:       "Frank finished MemPalace finally.",
			ents:      mkEnts([3]any{"Frank", "person", 0}, [3]any{"MemPalace", "project", 15}),
			wantSPO:   [3]string{"Frank", "finished", "MemPalace"},
			predicate: "finished",
		},
		{
			name:      "finished_miss_no_entities",
			doc:       "Frank finished MemPalace finally.",
			ents:      nil,
			wantSPO:   [3]string{"", "", ""},
			predicate: "finished",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := makeDrawer("drawer_1", tc.doc)
			rows := kg.AutoExtractTriples(d, tc.ents)
			wantEmpty := tc.wantSPO[0] == "" && tc.wantSPO[1] == "" && tc.wantSPO[2] == ""
			// Filter rows to the predicate under test so unrelated matches
			// from other patterns (cross-verb noise) don't confuse assertions.
			got := filterByPredicate(rows, tc.predicate)
			if wantEmpty {
				if len(got) != 0 {
					t.Errorf("want no triple for predicate %q, got %+v", tc.predicate, got)
				}
				return
			}
			if len(got) != 1 {
				t.Fatalf("want 1 triple for predicate %q, got %d (%+v)", tc.predicate, len(got), got)
			}
			if got[0].Subject != tc.wantSPO[0] ||
				got[0].Predicate != tc.wantSPO[1] ||
				got[0].Object != tc.wantSPO[2] {
				t.Errorf("triple = (%s, %s, %s); want (%s, %s, %s)",
					got[0].Subject, got[0].Predicate, got[0].Object,
					tc.wantSPO[0], tc.wantSPO[1], tc.wantSPO[2])
			}
		})
	}
}

func filterByPredicate(rows []palace.TripleRow, pred string) []palace.TripleRow {
	var out []palace.TripleRow
	for _, r := range rows {
		if r.Predicate == pred {
			out = append(out, r)
		}
	}
	return out
}

func TestAutoExtract_Idempotent(t *testing.T) {
	d := makeDrawer("drawer_1", "Alice works at Acme. Bob uses Python.")
	ents := mkEnts(
		[3]any{"Alice", "person", 0},
		[3]any{"Acme", "project", 15},
		[3]any{"Bob", "person", 21},
		[3]any{"Python", "tool", 30},
	)
	a := kg.AutoExtractTriples(d, ents)
	b := kg.AutoExtractTriples(d, ents)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("non-idempotent:\nfirst:  %+v\nsecond: %+v", a, b)
	}
}

func TestAutoExtract_ConfidenceConstant(t *testing.T) {
	d := makeDrawer("drawer_1", "Alice works at Acme.")
	ents := mkEnts([3]any{"Alice", "person", 0}, [3]any{"Acme", "project", 15})
	rows := kg.AutoExtractTriples(d, ents)
	if len(rows) == 0 {
		t.Fatal("expected at least one triple")
	}
	for _, r := range rows {
		if r.Confidence != kg.DefaultExtractConfidence {
			t.Errorf("confidence = %f; want %f", r.Confidence, kg.DefaultExtractConfidence)
		}
	}
}

func TestAutoExtract_ValidFromDateFormat(t *testing.T) {
	d := makeDrawer("drawer_1", "Alice works at Acme.")
	ents := mkEnts([3]any{"Alice", "person", 0}, [3]any{"Acme", "project", 15})
	rows := kg.AutoExtractTriples(d, ents)
	if len(rows) == 0 {
		t.Fatal("expected at least one triple")
	}
	for _, r := range rows {
		if r.ValidFrom != "2026-04-17" {
			t.Errorf("ValidFrom = %q; want 2026-04-17 (YYYY-MM-DD from FiledAt)", r.ValidFrom)
		}
	}
}

func TestAutoExtract_SourceCloset(t *testing.T) {
	d := makeDrawer("drawer_xyz", "Alice works at Acme.")
	ents := mkEnts([3]any{"Alice", "person", 0}, [3]any{"Acme", "project", 15})
	rows := kg.AutoExtractTriples(d, ents)
	if len(rows) == 0 {
		t.Fatal("expected at least one triple")
	}
	for _, r := range rows {
		if r.SourceCloset != "drawer_xyz" {
			t.Errorf("SourceCloset = %q; want %q", r.SourceCloset, "drawer_xyz")
		}
	}
}

func TestAutoExtract_Dedup(t *testing.T) {
	// Two surface forms for the same predicate ("works at" / "is at") hitting
	// the same subject/object should only emit once.
	d := makeDrawer("drawer_1", "Alice works at Acme. Alice is at Acme.")
	ents := mkEnts(
		[3]any{"Alice", "person", 0},
		[3]any{"Acme", "project", 15},
		[3]any{"Alice", "person", 21},
		[3]any{"Acme", "project", 33},
	)
	rows := kg.AutoExtractTriples(d, ents)
	count := 0
	for _, r := range rows {
		if r.Subject == "Alice" && r.Predicate == "works_at" && r.Object == "Acme" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 dedup'd triple for (Alice, works_at, Acme), got %d", count)
	}
}

func TestAutoExtract_EmptyInputs(t *testing.T) {
	d := makeDrawer("drawer_1", "")
	if got := kg.AutoExtractTriples(d, nil); len(got) != 0 {
		t.Errorf("empty doc should emit nothing, got %+v", got)
	}
	d2 := makeDrawer("drawer_1", "Alice works at Acme.")
	if got := kg.AutoExtractTriples(d2, nil); len(got) != 0 {
		t.Errorf("nil ents should emit nothing, got %+v", got)
	}
}

// pad returns a string of n space bytes. Used to push entity offsets beyond
// the maxGapBytes window so cross-sentence test cases really miss.
func pad(n int) string {
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}

func TestVerbPatterns_Exposed(t *testing.T) {
	if len(kg.VerbPatterns) != 6 {
		t.Errorf("VerbPatterns has %d entries; want 6", len(kg.VerbPatterns))
	}
	seen := map[string]bool{}
	for _, vp := range kg.VerbPatterns {
		if seen[vp.Predicate] {
			t.Errorf("duplicate predicate %q", vp.Predicate)
		}
		seen[vp.Predicate] = true
		if len(vp.SurfaceForms) == 0 {
			t.Errorf("predicate %q has no surface forms", vp.Predicate)
		}
	}
}
