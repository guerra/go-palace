//go:build integration

package testsuite_test

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-palace/pkg/embed"
	"go-palace/pkg/layers"
	"go-palace/pkg/palace"
)

// newSemanticEmbedder mirrors internal/embed/hugot_test.go:newTestEmbedder.
func newSemanticEmbedder(t *testing.T) *embed.HugotEmbedder {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: requires model download (~90MB)")
	}
	e, err := embed.NewHugotEmbedder(embed.HugotOptions{})
	if err != nil {
		t.Skipf("skipping: model unavailable: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

// newSemanticPalace creates a temp palace, loads all 4 fixtures, returns palace.
// Cleanup closes palace before temp dir removal (LIFO).
func newSemanticPalace(t *testing.T, e *embed.HugotEmbedder) *palace.Palace {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	p, err := palace.Open(dbPath, e)
	if err != nil {
		t.Fatalf("palace.Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	fixtures := []string{"pricing", "technical", "diary", "codereview"}
	for _, name := range fixtures {
		doc := loadFixture(t, name+".txt")
		err := p.Upsert(palace.Drawer{
			ID:       "fixture-" + name,
			Document: doc,
			Wing:     "test",
			Room:     name,
			AddedBy:  "semantic_test",
		})
		if err != nil {
			t.Fatalf("Upsert %s: %v", name, err)
		}
	}
	return p
}

func loadFixture(t *testing.T, name string) string {
	t.Helper()
	path := filepath.Join("..", "testdata", "fixtures", "semantic", name)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("fixture %s not found at %s: %v", name, path, err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return strings.TrimSpace(string(data))
}

func cosine(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		dot += float64(a[i]) * float64(b[i])
		normA += float64(a[i]) * float64(a[i])
		normB += float64(b[i]) * float64(b[i])
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// --- Task 3: Search relevance tests ---

func TestSemantic_SearchRelevance(t *testing.T) {
	e := newSemanticEmbedder(t)
	p := newSemanticPalace(t, e)

	cases := []struct {
		query       string
		wantTopRoom string
	}{
		{"pricing discussion and billing", "pricing"},
		{"database schema design", "technical"},
		{"weekend hiking trip", "diary"},
		{"code review feedback", "codereview"},
	}

	for _, tc := range cases {
		t.Run(tc.wantTopRoom, func(t *testing.T) {
			results, err := p.Query(tc.query, palace.QueryOptions{NResults: 4})
			if err != nil {
				t.Fatalf("Query(%q): %v", tc.query, err)
			}
			if len(results) == 0 {
				t.Fatalf("Query(%q): no results", tc.query)
			}
			if results[0].Drawer.Room != tc.wantTopRoom {
				t.Errorf("Query(%q): top result room=%q, want %q (sim=%f)",
					tc.query, results[0].Drawer.Room, tc.wantTopRoom, results[0].Similarity)
			}
			if results[0].Similarity < 0.3 {
				t.Errorf("Query(%q): top similarity=%f, want > 0.3",
					tc.query, results[0].Similarity)
			}
			t.Logf("top=%s sim=%.4f", results[0].Drawer.Room, results[0].Similarity)
		})
	}
}

func TestSemantic_SearchRanking(t *testing.T) {
	e := newSemanticEmbedder(t)
	p := newSemanticPalace(t, e)

	results, err := p.Query("pricing", palace.QueryOptions{NResults: 4})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}

	var pricingSim, diarySim float64
	for _, r := range results {
		switch r.Drawer.Room {
		case "pricing":
			pricingSim = r.Similarity
		case "diary":
			diarySim = r.Similarity
		}
	}
	if pricingSim <= diarySim {
		t.Errorf("pricing sim (%.4f) should be > diary sim (%.4f)", pricingSim, diarySim)
	}
	t.Logf("pricing=%.4f diary=%.4f delta=%.4f", pricingSim, diarySim, pricingSim-diarySim)
}

// --- Task 4: Embedding similarity and duplicate detection ---

func TestSemantic_SimilarityThresholds(t *testing.T) {
	e := newSemanticEmbedder(t)

	vecs, err := e.Embed([]string{
		"We need to discuss the pricing for enterprise customers",
		"Let's talk about enterprise pricing plans",
		"I went hiking in the mountains last weekend",
	})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}

	closeSim := cosine(vecs[0], vecs[1])
	farSim := cosine(vecs[0], vecs[2])

	if closeSim < 0.7 {
		t.Errorf("paraphrase cosine=%f, want > 0.7", closeSim)
	}
	if farSim > 0.4 {
		t.Errorf("unrelated cosine=%f, want < 0.4", farSim)
	}
	t.Logf("close=%.4f far=%.4f", closeSim, farSim)
}

func TestSemantic_DuplicateDetection(t *testing.T) {
	e := newSemanticEmbedder(t)
	dir := t.TempDir()
	p, err := palace.Open(filepath.Join(dir, "dup.db"), e)
	if err != nil {
		t.Fatalf("palace.Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	original := "Our SaaS pricing model includes three tiers with monthly and annual billing options and enterprise volume discounts."
	err = p.Upsert(palace.Drawer{
		ID:       "dup-original",
		Document: original,
		Wing:     "test",
		Room:     "pricing",
		AddedBy:  "semantic_test",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	paraphrase := "We offer a three-tier SaaS subscription model with both monthly and yearly billing, including discounts for enterprise volume purchases."
	results, err := p.Query(paraphrase, palace.QueryOptions{NResults: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results for paraphrase query")
	}
	if results[0].Similarity < 0.8 {
		t.Errorf("duplicate detection: sim=%f, want >= 0.8 (check_duplicate threshold)", results[0].Similarity)
	}
	t.Logf("duplicate sim=%.4f", results[0].Similarity)
}

func TestSemantic_NonDuplicateRejection(t *testing.T) {
	e := newSemanticEmbedder(t)
	dir := t.TempDir()
	p, err := palace.Open(filepath.Join(dir, "nodup.db"), e)
	if err != nil {
		t.Fatalf("palace.Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	err = p.Upsert(palace.Drawer{
		ID:       "nodup-pricing",
		Document: "Our SaaS pricing model includes three tiers with monthly and annual billing options and enterprise volume discounts.",
		Wing:     "test",
		Room:     "pricing",
		AddedBy:  "semantic_test",
	})
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	unrelated := "Saturday morning I hiked up the mountain trail and spent the evening cooking lamb stew while reading a novel."
	results, err := p.Query(unrelated, palace.QueryOptions{NResults: 1})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("no results")
	}
	if results[0].Similarity >= 0.5 {
		t.Errorf("non-duplicate: sim=%f, want < 0.5", results[0].Similarity)
	}
	t.Logf("non-duplicate sim=%.4f", results[0].Similarity)
}

// --- Task 5: Layer stack tests ---

func TestSemantic_L1EssentialStory(t *testing.T) {
	e := newSemanticEmbedder(t)
	p := newSemanticPalace(t, e)

	stack := layers.NewStack(p, "")
	l1 := stack.L1()

	if !strings.Contains(l1, "L1") {
		t.Error("L1 output missing L1 header")
	}
	if len(l1) < 50 {
		t.Errorf("L1 output too short (%d chars), expected fixture content", len(l1))
	}
	t.Logf("L1 length=%d chars", len(l1))
}

func TestSemantic_WakeUp(t *testing.T) {
	e := newSemanticEmbedder(t)
	p := newSemanticPalace(t, e)

	identityFile := filepath.Join(t.TempDir(), "identity.txt")
	if err := os.WriteFile(identityFile, []byte("Test Identity"), 0o644); err != nil {
		t.Fatalf("write identity: %v", err)
	}

	stack := layers.NewStack(p, identityFile)
	wakeup := stack.WakeUp()

	if !strings.Contains(wakeup, "Test Identity") {
		t.Error("WakeUp missing L0 identity text")
	}
	if !strings.Contains(wakeup, "L1") {
		t.Error("WakeUp missing L1 content")
	}
	t.Logf("WakeUp length=%d chars", len(wakeup))
}

func TestSemantic_L3Search(t *testing.T) {
	e := newSemanticEmbedder(t)
	p := newSemanticPalace(t, e)

	stack := layers.NewStack(p, "")
	l3 := stack.L3("pricing", "", "")

	if !strings.Contains(l3, "L3") {
		t.Error("L3 output missing L3 header")
	}
	if !strings.Contains(l3, "sim=") {
		t.Error("L3 output missing similarity scores")
	}
	if !strings.Contains(l3, "pricing") {
		t.Error("L3 output missing pricing content")
	}
	t.Logf("L3 output:\n%s", l3)
}
