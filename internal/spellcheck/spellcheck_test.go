package spellcheck

import (
	"strings"
	"testing"
)

func TestShouldSkipDigit(t *testing.T) {
	if !ShouldSkip("3am1", nil) {
		t.Error("expected skip for token with digit")
	}
}

func TestShouldSkipCamelCase(t *testing.T) {
	if !ShouldSkip("ChromaDB", nil) {
		t.Error("expected skip for CamelCase")
	}
}

func TestShouldSkipAllCaps(t *testing.T) {
	if !ShouldSkip("NDCG", nil) {
		t.Error("expected skip for ALL_CAPS")
	}
}

func TestShouldSkipTechnical(t *testing.T) {
	if !ShouldSkip("bge-large", nil) {
		t.Error("expected skip for technical token with hyphen")
	}
}

func TestShouldSkipShort(t *testing.T) {
	if !ShouldSkip("ok", nil) {
		t.Error("expected skip for short token")
	}
}

func TestShouldSkipKnown(t *testing.T) {
	known := map[string]bool{"riley": true}
	if !ShouldSkip("Riley", known) {
		t.Error("expected skip for known name")
	}
}

func TestShouldSkipNormal(t *testing.T) {
	if ShouldSkip("hello", nil) {
		t.Error("expected not to skip normal word")
	}
}

func TestEditDistanceSame(t *testing.T) {
	if got := EditDistance("hello", "hello"); got != 0 {
		t.Errorf("same string: got %d, want 0", got)
	}
}

func TestEditDistanceEmpty(t *testing.T) {
	if got := EditDistance("", "abc"); got != 3 {
		t.Errorf("empty vs abc: got %d, want 3", got)
	}
}

func TestEditDistanceKittenSitting(t *testing.T) {
	if got := EditDistance("kitten", "sitting"); got != 3 {
		t.Errorf("kitten/sitting: got %d, want 3", got)
	}
}

func TestShouldSkipURL(t *testing.T) {
	if !ShouldSkip("https://example.com", nil) {
		t.Error("expected skip for URL")
	}
}

func TestShouldSkipCodeEmoji(t *testing.T) {
	if !ShouldSkip("`code`", nil) {
		t.Error("expected skip for code/emoji token")
	}
}

// --- Spell correction tests ---

func TestSpellcheckCorrectsTypos(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"knoe", "know"},
		{"befor", "before"},
	}
	for _, tt := range tests {
		got := SpellcheckUserText(tt.input, nil)
		if got != tt.want {
			t.Errorf("SpellcheckUserText(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestSpellcheckPreservesCamelCase(t *testing.T) {
	got := SpellcheckUserText("ChromaDB is great", nil)
	if !strings.Contains(got, "ChromaDB") {
		t.Errorf("CamelCase should be preserved, got %q", got)
	}
}

func TestSpellcheckPreservesAllCaps(t *testing.T) {
	got := SpellcheckUserText("NDCG score", nil)
	if !strings.Contains(got, "NDCG") {
		t.Errorf("ALL_CAPS should be preserved, got %q", got)
	}
}

func TestSpellcheckPreservesTechnical(t *testing.T) {
	got := SpellcheckUserText("use bge-large model", nil)
	if !strings.Contains(got, "bge-large") {
		t.Errorf("technical token should be preserved, got %q", got)
	}
}

func TestSpellcheckPreservesKnownNames(t *testing.T) {
	known := map[string]bool{"riley": true}
	got := SpellcheckUserText("Riley went home", known)
	if !strings.Contains(got, "Riley") {
		t.Errorf("known name should be preserved, got %q", got)
	}
}

func TestSpellcheckPreservesValidWords(t *testing.T) {
	// "question" is a common word in the dictionary - should not be changed
	got := SpellcheckUserText("question about this", nil)
	if !strings.Contains(got, "question") {
		t.Errorf("valid dict word should be preserved, got %q", got)
	}
}

func TestSpellcheckPreservesCapitalized(t *testing.T) {
	got := SpellcheckUserText("Mempalace is useful", nil)
	if !strings.Contains(got, "Mempalace") {
		t.Errorf("capitalized word should be preserved, got %q", got)
	}
}

func TestSpellcheckPunctuationPreserved(t *testing.T) {
	got := SpellcheckUserText("befor.", nil)
	if got != "before." {
		t.Errorf("punctuation should be preserved: got %q, want %q", got, "before.")
	}
}

func TestSpellcheckEditDistanceGuard(t *testing.T) {
	// A word with no close correction should be returned as-is
	got := SpellcheckUserText("xyzqwkj", nil)
	if got != "xyzqwkj" {
		t.Errorf("distant word should be unchanged, got %q", got)
	}
}

func TestSpellcheckEmptyInput(t *testing.T) {
	if got := SpellcheckUserText("", nil); got != "" {
		t.Errorf("empty input should return empty, got %q", got)
	}
	if got := SpellcheckUserText("   ", nil); got != "   " {
		t.Errorf("whitespace should be preserved, got %q", got)
	}
}

func TestSpellcheckTranscript(t *testing.T) {
	input := "> befor the meeting\nassistant response\n> knoe the answer"
	got := SpellcheckTranscript(input)
	lines := strings.Split(got, "\n")
	// User lines get corrected
	if !strings.Contains(lines[0], "before") {
		t.Errorf("user line should be corrected: %q", lines[0])
	}
	// Assistant line untouched
	if lines[1] != "assistant response" {
		t.Errorf("assistant line should be unchanged: %q", lines[1])
	}
}

func TestSpellcheckTranscriptOnlyTouchesUserLines(t *testing.T) {
	input := "assistant line stays\n> user line\nmore assistant"
	got := SpellcheckTranscript(input)
	lines := strings.Split(got, "\n")
	if lines[0] != "assistant line stays" {
		t.Errorf("assistant line changed: %q", lines[0])
	}
	if lines[2] != "more assistant" {
		t.Errorf("second assistant line changed: %q", lines[2])
	}
}
