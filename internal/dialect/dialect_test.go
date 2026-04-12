package dialect

import (
	"strings"
	"testing"
)

func TestCompress(t *testing.T) {
	d := New(nil, nil)
	text := "We decided to use GraphQL instead of REST because it gives us better flexibility"
	result := d.Compress(text)
	if result == "" {
		t.Error("Compress returned empty string")
	}
	// Should contain content line with "0:" prefix
	if !strings.Contains(result, "0:") {
		t.Errorf("expected content line with '0:' prefix, got %q", result)
	}
	// Should detect DECISION flag (keyword "decided")
	if !strings.Contains(result, "DECISION") {
		t.Errorf("expected DECISION flag, got %q", result)
	}
}

func TestCompressWithMetadata(t *testing.T) {
	d := New(nil, nil)
	meta := map[string]string{
		"wing":        "conversations",
		"room":        "general",
		"date":        "2026-04-11",
		"source_file": "chat_001.txt",
	}
	result := d.Compress("Some text about a topic", meta)
	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines (header+content), got %d", len(lines))
	}
	// Header should contain wing|room|date|stem
	if !strings.Contains(lines[0], "conversations") {
		t.Errorf("header should contain wing, got %q", lines[0])
	}
	if !strings.Contains(lines[0], "chat_001") {
		t.Errorf("header should contain file stem, got %q", lines[0])
	}
}

func TestCompressEmptyText(t *testing.T) {
	d := New(nil, nil)
	result := d.Compress("")
	if !strings.Contains(result, "0:???") {
		t.Errorf("empty text should produce '0:???' entities, got %q", result)
	}
}

func TestDetectEmotions(t *testing.T) {
	d := New(nil, nil)
	emotions := d.detectEmotions("I was worried and excited about the change")
	if len(emotions) == 0 {
		t.Error("expected at least one emotion detected")
	}
	found := strings.Join(emotions, " ")
	if !strings.Contains(found, "anx") && !strings.Contains(found, "excite") {
		t.Errorf("expected anx or excite in emotions, got %v", emotions)
	}
}

func TestDetectFlags(t *testing.T) {
	d := New(nil, nil)
	flags := d.detectFlags("We decided to switch the database architecture")
	if len(flags) == 0 {
		t.Error("expected at least one flag detected")
	}
	found := strings.Join(flags, " ")
	if !strings.Contains(found, "DECISION") && !strings.Contains(found, "TECHNICAL") {
		t.Errorf("expected DECISION or TECHNICAL flag, got %v", flags)
	}
}

func TestExtractTopics(t *testing.T) {
	d := New(nil, nil)
	topics := d.extractTopics("GraphQL provides better flexibility than REST for API design", 3)
	if len(topics) == 0 {
		t.Error("expected at least one topic extracted")
	}
	// "graphql" should be boosted as a proper noun
	found := strings.Join(topics, " ")
	if !strings.Contains(found, "graphql") && !strings.Contains(found, "flexibility") && !strings.Contains(found, "rest") {
		t.Errorf("expected relevant topics, got %v", topics)
	}
}

func TestExtractKeySentence(t *testing.T) {
	d := New(nil, nil)
	text := "This is filler. We decided to switch because performance matters. Another sentence here about nothing."
	sent := d.extractKeySentence(text)
	if sent == "" {
		t.Error("expected a key sentence")
	}
	// The sentence with "decided" and "because" should score highest
	if !strings.Contains(strings.ToLower(sent), "decided") && !strings.Contains(strings.ToLower(sent), "because") {
		t.Errorf("expected sentence with decision words, got %q", sent)
	}
}

func TestDetectEntities(t *testing.T) {
	d := New(map[string]string{"Alice": "ALC"}, nil)
	entities := d.detectEntitiesInText("Alice and Bob discussed the plan")
	if len(entities) == 0 {
		t.Error("expected at least one entity")
	}
	if entities[0] != "ALC" {
		t.Errorf("expected ALC for Alice, got %q", entities[0])
	}
}

func TestDetectEntitiesFallback(t *testing.T) {
	d := New(nil, nil)
	entities := d.detectEntitiesInText("then Bob and Charlie went to the store")
	if len(entities) == 0 {
		t.Error("expected capitalized name detection fallback")
	}
}

func TestDecode(t *testing.T) {
	d := New(nil, nil)
	text := "We decided to use GraphQL instead of REST because flexibility"
	compressed := d.Compress(text)
	decoded := d.Decode(compressed)
	if len(decoded.Zettels) == 0 {
		t.Error("expected at least one zettel line in decoded output")
	}
}

func TestEncodeEntity(t *testing.T) {
	d := New(map[string]string{"Alice": "ALC", "Bob": "BOB"}, []string{"gandalf"})
	if got := d.EncodeEntity("Alice"); got != "ALC" {
		t.Errorf("EncodeEntity(Alice) = %q, want ALC", got)
	}
	if got := d.EncodeEntity("Charlie"); got != "CHA" {
		t.Errorf("EncodeEntity(Charlie) = %q, want CHA (auto-code)", got)
	}
	if got := d.EncodeEntity("Gandalf"); got != "" {
		t.Errorf("EncodeEntity(Gandalf) should be empty (skip name), got %q", got)
	}
}

func TestCompressionStats(t *testing.T) {
	d := New(nil, nil)
	original := "This is a long piece of text that should compress down to something shorter in the AAAK format."
	compressed := d.Compress(original)
	stats := d.CompressionStats(original, compressed)
	if stats.OriginalTokensEst <= 0 || stats.SummaryTokensEst <= 0 {
		t.Errorf("token estimates should be positive: orig=%d, summ=%d",
			stats.OriginalTokensEst, stats.SummaryTokensEst)
	}
}
