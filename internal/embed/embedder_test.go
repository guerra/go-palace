package embed

import "testing"

func TestFakeEmbedder_Dimension(t *testing.T) {
	e := NewFakeEmbedder(128)
	if got := e.Dimension(); got != 128 {
		t.Fatalf("dimension: got %d, want 128", got)
	}
}

func TestFakeEmbedder_DefaultDimension(t *testing.T) {
	e := NewFakeEmbedder(0)
	if got := e.Dimension(); got != 384 {
		t.Fatalf("default dimension: got %d, want 384", got)
	}
}

func TestFakeEmbedder_Deterministic(t *testing.T) {
	e := NewFakeEmbedder(64)
	a, err := e.Embed([]string{"hello world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	b, err := e.Embed([]string{"hello world"})
	if err != nil {
		t.Fatalf("embed: %v", err)
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

func TestFakeEmbedder_Batch(t *testing.T) {
	e := NewFakeEmbedder(32)
	vecs, err := e.Embed([]string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 3 {
		t.Fatalf("expected 3 vectors, got %d", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 32 {
			t.Fatalf("vec[%d]: len %d, want 32", i, len(v))
		}
	}
	if equalVec(vecs[0], vecs[1]) {
		t.Errorf("different inputs produced identical vectors")
	}
}

func TestFakeEmbedder_NilInput(t *testing.T) {
	e := NewFakeEmbedder(16)
	if _, err := e.Embed(nil); err == nil {
		t.Fatal("expected error on nil input")
	}
}

func equalVec(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
