package main

import (
	"math"
	"testing"
)

const epsilon = 1e-9

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < epsilon
}

func TestDCG(t *testing.T) {
	// [1, 0, 1] at k=3 -> 1/log2(2) + 0/log2(3) + 1/log2(4)
	// = 1.0 + 0.0 + 0.5 = 1.5
	got := DCG([]float64{1, 0, 1}, 3)
	want := 1.0/math.Log2(2) + 0.0 + 1.0/math.Log2(4)
	if !almostEqual(got, want) {
		t.Errorf("DCG([1,0,1], 3) = %f, want %f", got, want)
	}

	// k > len(relevances): should not panic.
	got2 := DCG([]float64{1}, 5)
	want2 := 1.0 / math.Log2(2)
	if !almostEqual(got2, want2) {
		t.Errorf("DCG([1], 5) = %f, want %f", got2, want2)
	}

	// Empty.
	if DCG(nil, 3) != 0.0 {
		t.Error("DCG(nil, 3) should be 0")
	}
}

func TestNDCG_Perfect(t *testing.T) {
	// Perfect ranking: gold at index 0, corpus = [gold, other, other].
	corpus := []string{"gold", "a", "b"}
	correct := map[string]bool{"gold": true}
	rankings := []int{0, 1, 2}

	got := NDCG(rankings, correct, corpus, 3)
	if !almostEqual(got, 1.0) {
		t.Errorf("NDCG perfect = %f, want 1.0", got)
	}
}

func TestNDCG_Worst(t *testing.T) {
	// Gold at last position.
	corpus := []string{"a", "b", "gold"}
	correct := map[string]bool{"gold": true}
	rankings := []int{0, 1, 2} // gold at rank 3

	got := NDCG(rankings, correct, corpus, 3)
	// DCG = 0 + 0 + 1/log2(4) = 0.5; IDCG = 1/log2(2) = 1.0
	want := (1.0 / math.Log2(4)) / (1.0 / math.Log2(2))
	if !almostEqual(got, want) {
		t.Errorf("NDCG worst = %f, want %f", got, want)
	}
}

func TestNDCG_NoRelevant(t *testing.T) {
	corpus := []string{"a", "b", "c"}
	correct := map[string]bool{"gold": true}
	rankings := []int{0, 1, 2}

	got := NDCG(rankings, correct, corpus, 3)
	if got != 0.0 {
		t.Errorf("NDCG no-relevant = %f, want 0.0", got)
	}
}

func TestEvaluateRetrieval_Hit(t *testing.T) {
	corpus := []string{"gold", "a", "b", "c", "d"}
	correct := map[string]bool{"gold": true}
	rankings := []int{1, 0, 2, 3, 4} // gold at rank 2

	recall, ndcg := EvaluateRetrieval(rankings, correct, corpus, 3)
	if recall != 1.0 {
		t.Errorf("recall = %f, want 1.0", recall)
	}
	if ndcg <= 0 {
		t.Error("ndcg should be > 0")
	}
}

func TestEvaluateRetrieval_Miss(t *testing.T) {
	corpus := []string{"gold", "a", "b", "c", "d"}
	correct := map[string]bool{"gold": true}
	rankings := []int{1, 2, 3, 4, 0} // gold at rank 5

	recall, _ := EvaluateRetrieval(rankings, correct, corpus, 3)
	if recall != 0.0 {
		t.Errorf("recall = %f, want 0.0", recall)
	}
}

func TestEvaluateRetrieval_KGreaterThanRankings(t *testing.T) {
	corpus := []string{"gold", "a"}
	correct := map[string]bool{"gold": true}
	rankings := []int{1, 0}

	recall, _ := EvaluateRetrieval(rankings, correct, corpus, 10)
	if recall != 1.0 {
		t.Errorf("recall = %f, want 1.0", recall)
	}
}

func TestEvaluateRetrieval_EmptyCorrectIDs(t *testing.T) {
	corpus := []string{"a", "b", "c"}
	correct := map[string]bool{}
	rankings := []int{0, 1, 2}

	recall, ndcg := EvaluateRetrieval(rankings, correct, corpus, 3)
	if recall != 0.0 {
		t.Errorf("recall = %f, want 0.0", recall)
	}
	if ndcg != 0.0 {
		t.Errorf("ndcg = %f, want 0.0", ndcg)
	}
}
