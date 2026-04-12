// Package sanitizer mitigates system prompt contamination in search queries.
//
// AI agents sometimes prepend system prompts (2000+ chars) to search queries.
// Embedding models represent the concatenation as a single vector where the
// system prompt overwhelms the actual question, causing near-total retrieval
// failure. This package extracts the real query via a 4-step pipeline.
//
// Length constants are in runes (characters), matching Python's len() semantics.
package sanitizer

import (
	"log/slog"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	MaxQueryLength  = 500
	SafeQueryLength = 200
	MinQueryLength  = 10
)

// sentence splitter: split on . ! ? (including fullwidth) and newlines
var sentenceSplit = regexp.MustCompile(`[.!?` + "\u3002\uff01\uff1f" + `\n]+`)

// question detector: ends with ? or fullwidth ? (possibly with trailing whitespace/quotes)
var questionMark = regexp.MustCompile(`[?` + "\uff1f" + `]\s*["']?\s*$`)

// SanitizeResult holds the output of the sanitization pipeline.
type SanitizeResult struct {
	CleanQuery     string
	WasSanitized   bool
	OriginalLength int
	CleanLength    int
	Method         string
}

// SanitizeQuery extracts the actual search intent from a potentially
// contaminated query string. Empty input is a passthrough.
func SanitizeQuery(rawQuery string) SanitizeResult {
	if rawQuery == "" || strings.TrimSpace(rawQuery) == "" {
		return SanitizeResult{
			CleanQuery:     rawQuery,
			WasSanitized:   false,
			OriginalLength: runeLen(rawQuery),
			CleanLength:    runeLen(rawQuery),
			Method:         "passthrough",
		}
	}

	rawQuery = strings.TrimSpace(rawQuery)
	originalLength := runeLen(rawQuery)

	// Step 1: Short query passthrough.
	if originalLength <= SafeQueryLength {
		return SanitizeResult{
			CleanQuery:     rawQuery,
			WasSanitized:   false,
			OriginalLength: originalLength,
			CleanLength:    originalLength,
			Method:         "passthrough",
		}
	}

	// Step 2: Question extraction.
	// Split into sentence fragments.
	sentences := splitNonEmpty(sentenceSplit.Split(rawQuery, -1))

	// Also split on newlines to catch questions on their own line.
	allSegments := splitNonEmpty(strings.Split(rawQuery, "\n"))

	// Look for question marks in segments (prefer later ones).
	var questionSentences []string
	for i := len(allSegments) - 1; i >= 0; i-- {
		if questionMark.MatchString(allSegments[i]) {
			questionSentences = append(questionSentences, allSegments[i])
		}
	}
	if len(questionSentences) == 0 {
		for i := len(sentences) - 1; i >= 0; i-- {
			if strings.Contains(sentences[i], "?") || strings.Contains(sentences[i], "\uff1f") {
				questionSentences = append(questionSentences, sentences[i])
			}
		}
	}

	if len(questionSentences) > 0 {
		candidate := strings.TrimSpace(questionSentences[0])
		if runeLen(candidate) >= MinQueryLength {
			candidate = truncTail(candidate, MaxQueryLength)
			slog.Warn("Query sanitized",
				"original_length", originalLength,
				"clean_length", runeLen(candidate),
				"method", "question_extraction")
			return SanitizeResult{
				CleanQuery:     candidate,
				WasSanitized:   true,
				OriginalLength: originalLength,
				CleanLength:    runeLen(candidate),
				Method:         "question_extraction",
			}
		}
	}

	// Step 3: Tail sentence extraction.
	for i := len(allSegments) - 1; i >= 0; i-- {
		seg := strings.TrimSpace(allSegments[i])
		if runeLen(seg) >= MinQueryLength {
			candidate := truncTail(seg, MaxQueryLength)
			slog.Warn("Query sanitized",
				"original_length", originalLength,
				"clean_length", runeLen(candidate),
				"method", "tail_sentence")
			return SanitizeResult{
				CleanQuery:     candidate,
				WasSanitized:   true,
				OriginalLength: originalLength,
				CleanLength:    runeLen(candidate),
				Method:         "tail_sentence",
			}
		}
	}

	// Step 4: Tail truncation (fallback).
	candidate := truncTail(rawQuery, MaxQueryLength)
	candidate = strings.TrimSpace(candidate)
	slog.Warn("Query sanitized",
		"original_length", originalLength,
		"clean_length", runeLen(candidate),
		"method", "tail_truncation")
	return SanitizeResult{
		CleanQuery:     candidate,
		WasSanitized:   true,
		OriginalLength: originalLength,
		CleanLength:    runeLen(candidate),
		Method:         "tail_truncation",
	}
}

// runeLen returns the number of runes (characters) in s, matching Python len().
func runeLen(s string) int {
	return utf8.RuneCountInString(s)
}

// truncTail returns the last maxRunes runes of s. If s has fewer runes, it
// is returned unchanged. Always produces valid UTF-8.
func truncTail(s string, maxRunes int) string {
	n := utf8.RuneCountInString(s)
	if n <= maxRunes {
		return s
	}
	// Skip (n - maxRunes) runes from the front.
	skip := n - maxRunes
	i := 0
	for skip > 0 {
		_, size := utf8.DecodeRuneInString(s[i:])
		i += size
		skip--
	}
	return s[i:]
}

// splitNonEmpty trims and filters out empty strings.
func splitNonEmpty(parts []string) []string {
	var out []string
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}
