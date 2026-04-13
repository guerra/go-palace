package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/palace"
)

type rankedItem struct {
	CorpusID  string `json:"corpus_id"`
	Text      string `json:"text"`
	Timestamp string `json:"timestamp"`
}

type resultEntry struct {
	QuestionID   string `json:"question_id"`
	QuestionType string `json:"question_type"`
	Question     string `json:"question"`
	Answer       string `json:"answer"`
	Retrieval    struct {
		Query       string       `json:"query"`
		RankedItems []rankedItem `json:"ranked_items"`
		Metrics     struct {
			Session map[string]float64 `json:"session"`
			Turn    map[string]float64 `json:"turn"`
		} `json:"metrics"`
	} `json:"retrieval_results"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func run() error {
	dataPath := flag.String("data", "longmemeval_s_cleaned.json", "path to dataset JSON")
	limit := flag.Int("limit", 0, "run first N questions only (0=all)")
	skip := flag.Int("skip", 0, "skip first N questions")
	topK := flag.Int("top-k", 50, "max results per query (eval at 1,3,5,10,30,50)")
	granularity := flag.String("granularity", "session", "retrieval granularity: session or turn")
	outPath := flag.String("out", "", "output JSONL file path")
	flag.Parse()

	dataFile, err := EnsureDataset(*dataPath)
	if err != nil {
		return fmt.Errorf("dataset: %w", err)
	}

	entries, err := LoadDataset(dataFile)
	if err != nil {
		return fmt.Errorf("load: %w", err)
	}

	if *skip > 0 && *skip < len(entries) {
		fmt.Printf("  Skipping first %d questions (resume mode)\n", *skip)
		entries = entries[*skip:]
	}
	if *limit > 0 && *limit < len(entries) {
		entries = entries[:*limit]
	}

	if *outPath == "" {
		*outPath = fmt.Sprintf("benchmarks/results_gopalace_raw_%s_%s.jsonl",
			*granularity, time.Now().Format("20060102_1504"))
	}

	emb, err := embed.NewHugotEmbedder(embed.HugotOptions{})
	if err != nil {
		return fmt.Errorf("embedder: %w", err)
	}
	defer emb.Close()

	ks := []int{1, 3, 5, 10, 30, 50}

	type metricAccum struct {
		recallAny []float64
		ndcg      []float64
	}

	sessionMetrics := make(map[int]*metricAccum)
	turnMetrics := make(map[int]*metricAccum)
	for _, k := range ks {
		sessionMetrics[k] = &metricAccum{}
		turnMetrics[k] = &metricAccum{}
	}

	perType := make(map[string]*struct {
		recallAny5  []float64
		recallAny10 []float64
		ndcgAny10   []float64
	})

	var resultsLog []resultEntry

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Println("  go-palace x LongMemEval Benchmark")
	fmt.Printf("%s\n", strings.Repeat("=", 60))
	fmt.Printf("  Questions:   %d\n", len(entries))
	fmt.Printf("  Granularity: %s\n", *granularity)
	fmt.Printf("  Mode:        raw\n")
	fmt.Printf("%s\n\n", strings.Repeat("-", 60))

	startTime := time.Now()
	evaluated := 0

	for i, entry := range entries {
		rankings, corpus, corpusIDs, corpusTimestamps, err := buildAndRetrieve(entry, emb, *granularity, *topK)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [%4d/%d] %s ERROR: %v\n", i+1, len(entries), entry.QuestionID, err)
			continue
		}
		if len(rankings) == 0 {
			fmt.Printf("  [%4d/%d] %-30s SKIP (empty corpus)\n", i+1, len(entries), truncate(entry.QuestionID, 30))
			continue
		}

		answerSIDs := toSet(entry.AnswerSessionIDs)

		// Session-level IDs (strip _turn_N suffix if turn granularity).
		sessionLevelIDs := make([]string, len(corpusIDs))
		for j, cid := range corpusIDs {
			sessionLevelIDs[j] = SessionIDFromCorpusID(cid)
		}

		// Turn-level correct: any corpus_id whose session part is in answer_sids.
		turnCorrect := make(map[string]bool)
		for _, cid := range corpusIDs {
			if answerSIDs[SessionIDFromCorpusID(cid)] {
				turnCorrect[cid] = true
			}
		}

		entrySessionMetrics := make(map[string]float64)
		entryTurnMetrics := make(map[string]float64)

		for _, k := range ks {
			ra, nd := EvaluateRetrieval(rankings, answerSIDs, sessionLevelIDs, k)
			sessionMetrics[k].recallAny = append(sessionMetrics[k].recallAny, ra)
			sessionMetrics[k].ndcg = append(sessionMetrics[k].ndcg, nd)
			entrySessionMetrics[fmt.Sprintf("recall_any@%d", k)] = ra
			entrySessionMetrics[fmt.Sprintf("ndcg_any@%d", k)] = nd

			raT, ndT := EvaluateRetrieval(rankings, turnCorrect, corpusIDs, k)
			turnMetrics[k].recallAny = append(turnMetrics[k].recallAny, raT)
			turnMetrics[k].ndcg = append(turnMetrics[k].ndcg, ndT)
			entryTurnMetrics[fmt.Sprintf("recall_any@%d", k)] = raT
			entryTurnMetrics[fmt.Sprintf("ndcg_any@%d", k)] = ndT
		}

		// Per-type tracking.
		pt, ok := perType[entry.QuestionType]
		if !ok {
			pt = &struct {
				recallAny5  []float64
				recallAny10 []float64
				ndcgAny10   []float64
			}{}
			perType[entry.QuestionType] = pt
		}
		pt.recallAny5 = append(pt.recallAny5, entrySessionMetrics["recall_any@5"])
		pt.recallAny10 = append(pt.recallAny10, entrySessionMetrics["recall_any@10"])
		pt.ndcgAny10 = append(pt.ndcgAny10, entrySessionMetrics["ndcg_any@10"])

		// JSONL log entry.
		var re resultEntry
		re.QuestionID = entry.QuestionID
		re.QuestionType = entry.QuestionType
		re.Question = entry.Question
		re.Answer = entry.Answer
		re.Retrieval.Query = entry.Question
		re.Retrieval.Metrics.Session = entrySessionMetrics
		re.Retrieval.Metrics.Turn = entryTurnMetrics

		maxItems := 50
		if maxItems > len(rankings) {
			maxItems = len(rankings)
		}
		for _, idx := range rankings[:maxItems] {
			text := corpus[idx]
			if len(text) > 500 {
				text = text[:500]
			}
			re.Retrieval.RankedItems = append(re.Retrieval.RankedItems, rankedItem{
				CorpusID:  corpusIDs[idx],
				Text:      text,
				Timestamp: corpusTimestamps[idx],
			})
		}
		resultsLog = append(resultsLog, re)

		r5 := entrySessionMetrics["recall_any@5"]
		r10 := entrySessionMetrics["recall_any@10"]
		status := "miss"
		if r5 > 0 {
			status = "HIT"
		}
		fmt.Printf("  [%4d/%d] %-30s R@5=%.0f R@10=%.0f  %s\n",
			i+1, len(entries), truncate(entry.QuestionID, 30), r5, r10, status)

		evaluated++
	}

	elapsed := time.Since(startTime).Seconds()

	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("  RESULTS -- go-palace (raw mode, %s granularity)\n", *granularity)
	fmt.Printf("%s\n", strings.Repeat("=", 60))
	if evaluated > 0 {
		fmt.Printf("  Time: %.1fs (%.2fs per question)\n\n", elapsed, elapsed/float64(evaluated))
	}

	fmt.Println("  SESSION-LEVEL METRICS:")
	for _, k := range ks {
		m := sessionMetrics[k]
		if len(m.recallAny) == 0 {
			continue
		}
		fmt.Printf("    Recall@%2d: %.3f    NDCG@%2d: %.3f\n", k, mean(m.recallAny), k, mean(m.ndcg))
	}

	fmt.Println("\n  TURN-LEVEL METRICS:")
	for _, k := range ks {
		m := turnMetrics[k]
		if len(m.recallAny) == 0 {
			continue
		}
		fmt.Printf("    Recall@%2d: %.3f    NDCG@%2d: %.3f\n", k, mean(m.recallAny), k, mean(m.ndcg))
	}

	fmt.Println("\n  PER-TYPE BREAKDOWN (session recall_any@10):")
	for qtype, pt := range perType {
		if len(pt.recallAny10) == 0 {
			continue
		}
		fmt.Printf("    %-35s R@10=%.3f  (n=%d)\n", qtype, mean(pt.recallAny10), len(pt.recallAny10))
	}

	fmt.Printf("\n%s\n\n", strings.Repeat("=", 60))

	// Write JSONL.
	if *outPath != "" && len(resultsLog) > 0 {
		if err := writeJSONL(*outPath, resultsLog); err != nil {
			return fmt.Errorf("write output: %w", err)
		}
		fmt.Printf("  Results saved to %s\n", *outPath)
	}

	return nil
}

func writeJSONL(path string, entries []resultEntry) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, r := range entries {
		if err := enc.Encode(r); err != nil {
			return err
		}
	}
	return nil
}

// buildAndRetrieve creates an ephemeral palace, ingests corpus, queries, and
// returns rankings as indices into the corpus slices.
func buildAndRetrieve(
	entry LongMemEntry,
	emb embed.Embedder,
	granularity string,
	nResults int,
) (rankings []int, corpus []string, corpusIDs []string, corpusTimestamps []string, err error) {
	// Build corpus.
	for sessIdx, session := range entry.HaystackSessions {
		sessID := entry.HaystackSessionIDs[sessIdx]
		date := entry.HaystackDates[sessIdx]

		if granularity == "session" {
			doc := JoinUserTurns(session)
			if doc != "" {
				corpus = append(corpus, doc)
				corpusIDs = append(corpusIDs, sessID)
				corpusTimestamps = append(corpusTimestamps, date)
			}
		} else {
			turnNum := 0
			for _, turn := range session {
				if turn.Role == "user" {
					corpus = append(corpus, turn.Content)
					corpusIDs = append(corpusIDs, fmt.Sprintf("%s_turn_%d", sessID, turnNum))
					corpusTimestamps = append(corpusTimestamps, date)
					turnNum++
				}
			}
		}
	}

	if len(corpus) == 0 {
		return nil, nil, nil, nil, nil
	}

	p, err := palace.Open(":memory:", emb)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("open palace: %w", err)
	}
	defer p.Close()

	// Build drawers.
	drawers := make([]palace.Drawer, len(corpus))
	for i, doc := range corpus {
		drawers[i] = palace.Drawer{
			ID:       corpusIDs[i],
			Document: doc,
		}
	}

	if err := p.UpsertBatch(drawers); err != nil {
		return nil, nil, nil, nil, fmt.Errorf("upsert: %w", err)
	}

	n := nResults
	if n > len(corpus) {
		n = len(corpus)
	}
	results, err := p.Query(entry.Question, palace.QueryOptions{NResults: n})
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("query: %w", err)
	}

	// Map results back to corpus indices.
	idToIdx := make(map[string]int, len(corpusIDs))
	for i, cid := range corpusIDs {
		idToIdx[cid] = i
	}

	seen := make(map[int]bool, len(results))
	for _, r := range results {
		if idx, ok := idToIdx[r.Drawer.ID]; ok {
			rankings = append(rankings, idx)
			seen[idx] = true
		}
	}

	// Fill missing indices (palace may return fewer than corpus size).
	for i := range corpus {
		if !seen[i] {
			rankings = append(rankings, i)
		}
	}

	return rankings, corpus, corpusIDs, corpusTimestamps, nil
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func mean(vs []float64) float64 {
	if len(vs) == 0 {
		return 0
	}
	sum := 0.0
	for _, v := range vs {
		sum += v
	}
	return sum / float64(len(vs))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
