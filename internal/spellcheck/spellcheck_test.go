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

func TestSpellcheckUserTextPassthrough(t *testing.T) {
	input := "this is some text with typos"
	got := SpellcheckUserText(input, nil)
	if got != input {
		t.Errorf("stub should return input unchanged, got %q", got)
	}
}

func TestSpellcheckTranscript(t *testing.T) {
	input := "> hello world\nassistant response\n> another question"
	got := SpellcheckTranscript(input)
	// With stub, output should be identical to input.
	if got != input {
		t.Errorf("transcript stub should return input unchanged, got %q", got)
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
