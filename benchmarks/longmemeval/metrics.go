package main

import (
	"math"
	"sort"
)

// DCG computes Discounted Cumulative Gain for the first k relevances.
// Formula: sum(rel_i / log2(i+2)) for i in 0..k-1
func DCG(relevances []float64, k int) float64 {
	score := 0.0
	limit := k
	if limit > len(relevances) {
		limit = len(relevances)
	}
	for i := 0; i < limit; i++ {
		score += relevances[i] / math.Log2(float64(i+2))
	}
	return score
}

// NDCG computes Normalized DCG at rank k.
// rankings are indices into corpusIDs, ordered by relevance (best first).
func NDCG(rankings []int, correctIDs map[string]bool, corpusIDs []string, k int) float64 {
	limit := k
	if limit > len(rankings) {
		limit = len(rankings)
	}

	relevances := make([]float64, limit)
	for i := 0; i < limit; i++ {
		if correctIDs[corpusIDs[rankings[i]]] {
			relevances[i] = 1.0
		}
	}

	ideal := make([]float64, len(relevances))
	copy(ideal, relevances)
	sort.Float64s(ideal)
	// Reverse to descending.
	for i, j := 0, len(ideal)-1; i < j; i, j = i+1, j-1 {
		ideal[i], ideal[j] = ideal[j], ideal[i]
	}

	idcg := DCG(ideal, k)
	if idcg == 0 {
		return 0.0
	}
	return DCG(relevances, k) / idcg
}

// EvaluateRetrieval computes recall-any and NDCG at rank k.
// Returns (recallAny, ndcg).
func EvaluateRetrieval(rankings []int, correctIDs map[string]bool, corpusIDs []string, k int) (float64, float64) {
	limit := k
	if limit > len(rankings) {
		limit = len(rankings)
	}

	topKIDs := make(map[string]bool, limit)
	for i := 0; i < limit; i++ {
		topKIDs[corpusIDs[rankings[i]]] = true
	}

	recallAny := 0.0
	for cid := range correctIDs {
		if topKIDs[cid] {
			recallAny = 1.0
			break
		}
	}

	ndcgScore := NDCG(rankings, correctIDs, corpusIDs, k)
	return recallAny, ndcgScore
}
