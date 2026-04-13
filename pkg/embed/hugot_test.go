//go:build integration

package embed_test

import (
	"math"
	"os"
	"testing"

	"github.com/guerra/go-palace/pkg/embed"
)

func newTestEmbedder(t *testing.T) *embed.HugotEmbedder {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping: requires model download (~90MB)")
	}
	e, err := embed.NewHugotEmbedder(embed.HugotOptions{})
	if err != nil {
		if os.Getenv("CI") != "" {
			t.Skip("skipping: model unavailable in CI")
		}
		t.Fatalf("NewHugotEmbedder: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })
	return e
}

func TestHugotEmbedder_Dimension(t *testing.T) {
	e := newTestEmbedder(t)
	if got := e.Dimension(); got != 384 {
		t.Fatalf("dimension: got %d, want 384", got)
	}
}

func TestHugotEmbedder_Determinism(t *testing.T) {
	e := newTestEmbedder(t)
	a, err := e.Embed([]string{"hello world"})
	if err != nil {
		t.Fatalf("embed1: %v", err)
	}
	b, err := e.Embed([]string{"hello world"})
	if err != nil {
		t.Fatalf("embed2: %v", err)
	}
	if len(a) != 1 || len(b) != 1 {
		t.Fatalf("expected 1 vector each, got %d and %d", len(a), len(b))
	}
	for i := range a[0] {
		if a[0][i] != b[0][i] {
			t.Fatalf("not deterministic at index %d: %f != %f", i, a[0][i], b[0][i])
		}
	}
}

func TestHugotEmbedder_Batch(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed([]string{"alpha", "beta", "gamma"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 384 {
			t.Fatalf("vec[%d]: len %d, want 384", i, len(v))
		}
	}
}

func TestHugotEmbedder_Similarity(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed([]string{
		"cat sat on mat",
		"dog sat on rug",
		"quantum physics equation",
	})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	simClose := cosine(vecs[0], vecs[1])
	simFar := cosine(vecs[0], vecs[2])
	if simClose < 0.5 {
		t.Errorf("similar texts: cosine=%f, want > 0.5", simClose)
	}
	if simFar >= simClose {
		t.Errorf("dissimilar pair (%f) should be < similar pair (%f)", simFar, simClose)
	}
}

func TestHugotEmbedder_NilInput(t *testing.T) {
	e := newTestEmbedder(t)
	if _, err := e.Embed(nil); err == nil {
		t.Fatal("expected error on nil input")
	}
}

func TestHugotEmbedder_EmptySlice(t *testing.T) {
	e := newTestEmbedder(t)
	vecs, err := e.Embed([]string{})
	if err != nil {
		t.Fatalf("embed empty: %v", err)
	}
	if len(vecs) != 0 {
		t.Errorf("expected 0 vectors, got %d", len(vecs))
	}
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
