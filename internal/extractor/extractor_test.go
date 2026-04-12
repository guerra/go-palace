package extractor

import (
	"strings"
	"testing"
)

func TestDecisionMarker(t *testing.T) {
	text := "We decided to use Go for this project because it has better concurrency support and compile times"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "decision" {
		t.Errorf("expected decision, got %s", mems[0].MemoryType)
	}
}

func TestPreferenceMarker(t *testing.T) {
	text := "I prefer tabs over spaces and always use functional style in my codebase whenever possible"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "preference" {
		t.Errorf("expected preference, got %s", mems[0].MemoryType)
	}
}

func TestMilestoneMarker(t *testing.T) {
	text := "It finally works! After three days of debugging I got it working and it was a real breakthrough moment"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "milestone" {
		t.Errorf("expected milestone, got %s", mems[0].MemoryType)
	}
}

func TestProblemMarker(t *testing.T) {
	text := "The bug was in the parser and it keeps crashing whenever we send malformed input to the endpoint"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "problem" {
		t.Errorf("expected problem, got %s", mems[0].MemoryType)
	}
}

func TestEmotionalMarker(t *testing.T) {
	text := "I love this project so much, it makes me happy every time I work on it and I feel grateful for the team"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "emotional" {
		t.Errorf("expected emotional, got %s", mems[0].MemoryType)
	}
}

func TestNoSignal(t *testing.T) {
	text := "hello world this is just some plain ordinary text"
	mems := ExtractMemories(text, 0.3)
	if len(mems) != 0 {
		t.Errorf("expected no memories, got %d: %+v", len(mems), mems)
	}
}

func TestMinConfidence(t *testing.T) {
	// Single weak marker, high confidence threshold should filter it out.
	text := "something about a default setting in the application configuration options"
	mems := ExtractMemories(text, 0.9)
	if len(mems) != 0 {
		t.Errorf("expected filtered out at 0.9 confidence, got %d", len(mems))
	}
}

func TestCodeLineDetection(t *testing.T) {
	if !IsCodeLine("$ npm install") {
		t.Error("expected code line for '$ npm install'")
	}
	if IsCodeLine("regular text that is normal prose") {
		t.Error("expected non-code for regular text")
	}
	if !IsCodeLine("import os") {
		t.Error("expected code line for 'import os'")
	}
}

func TestDisambiguateResolvedProblem(t *testing.T) {
	// Problem + resolution => milestone.
	text := "The bug was causing crashes but I fixed it and now it works perfectly fine again for everyone"
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory")
	}
	if mems[0].MemoryType != "milestone" {
		t.Errorf("expected milestone (resolved problem), got %s", mems[0].MemoryType)
	}
}

func TestLengthBonus(t *testing.T) {
	// >500 chars should get +2 bonus.
	text := "We decided to use Go " + strings.Repeat("for this very important project ", 20)
	if len(text) <= 500 {
		t.Fatal("test text must be >500 chars")
	}
	mems := ExtractMemories(text, 0.1)
	if len(mems) == 0 {
		t.Fatal("expected at least one memory with length bonus")
	}
}

func TestExtractProseSkipsCode(t *testing.T) {
	text := "I decided to rewrite the whole thing\n```\nfunc main() {\n}\n```\nBecause it was better"
	prose := extractProse(text)
	if strings.Contains(prose, "func main") {
		t.Error("prose should not contain code block content")
	}
}

func TestSplitIntoSegmentsTurns(t *testing.T) {
	text := "> question 1\nanswer 1\n> question 2\nanswer 2\n> question 3\nanswer 3"
	segments := splitIntoSegments(text)
	if len(segments) < 3 {
		t.Errorf("expected >= 3 segments from turn-based split, got %d", len(segments))
	}
}
