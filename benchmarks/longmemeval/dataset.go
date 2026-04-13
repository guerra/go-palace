package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

const datasetURL = "https://huggingface.co/datasets/xiaowu0162/longmemeval-cleaned/resolve/main/longmemeval_s_cleaned.json"

// Turn is a single dialogue turn in a haystack session.
type Turn struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// LongMemEntry is one question from the LongMemEval dataset.
type LongMemEntry struct {
	QuestionID         string   `json:"question_id"`
	QuestionType       string   `json:"question_type"`
	Question           string   `json:"question"`
	Answer             string   `json:"answer"`
	QuestionDate       string   `json:"question_date"`
	HaystackSessionIDs []string `json:"haystack_session_ids"`
	HaystackDates      []string `json:"haystack_dates"`
	HaystackSessions   [][]Turn `json:"haystack_sessions"`
	AnswerSessionIDs   []string `json:"answer_session_ids"`
}

// LoadDataset reads the JSON array of LongMemEntry from path.
func LoadDataset(path string) ([]LongMemEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open dataset: %w", err)
	}
	defer f.Close()

	var entries []LongMemEntry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decode dataset: %w", err)
	}
	return entries, nil
}

// EnsureDataset returns path if it exists, otherwise downloads from HuggingFace.
func EnsureDataset(path string) (string, error) {
	if _, err := os.Stat(path); err == nil {
		return path, nil
	}

	fmt.Printf("  Downloading dataset to %s ...\n", path)
	resp, err := http.Get(datasetURL)
	if err != nil {
		return "", fmt.Errorf("download dataset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download dataset: HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("create dataset file: %w", err)
	}
	defer out.Close()

	if _, err := io.Copy(out, resp.Body); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("write dataset: %w", err)
	}
	fmt.Println("  Download complete.")
	return path, nil
}

// IsAbstention returns true if the question is an abstention probe (ID ends with _abs).
func IsAbstention(entry LongMemEntry) bool {
	return strings.HasSuffix(entry.QuestionID, "_abs")
}

// JoinUserTurns concatenates user-role content from a session with newlines.
func JoinUserTurns(session []Turn) string {
	var parts []string
	for _, t := range session {
		if t.Role == "user" {
			parts = append(parts, t.Content)
		}
	}
	return strings.Join(parts, "\n")
}

// SessionIDFromCorpusID extracts the session ID from a corpus ID.
// Turn IDs look like "sess_123_turn_4" -- session part is "sess_123".
func SessionIDFromCorpusID(corpusID string) string {
	if idx := strings.LastIndex(corpusID, "_turn_"); idx >= 0 {
		return corpusID[:idx]
	}
	return corpusID
}
