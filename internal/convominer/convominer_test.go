package convominer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChunkExchangesWithMarkers(t *testing.T) {
	content := "> What is Go used for in practice?\nGo is commonly used for building servers, microservices, and CLI tools."
	chunks := ChunkExchanges(content)
	if len(chunks) != 1 {
		t.Fatalf("expected 1 chunk, got %d", len(chunks))
	}
	if !strings.HasPrefix(chunks[0].Content, "> What is Go") {
		t.Errorf("chunk should start with user turn: %s", chunks[0].Content)
	}
}

func TestChunkExchangesParagraphFallback(t *testing.T) {
	content := "First paragraph about something.\n\nSecond paragraph about another topic that has enough content to pass the filter."
	chunks := ChunkExchanges(content)
	if len(chunks) < 1 {
		t.Fatalf("expected at least 1 chunk from paragraph fallback, got %d", len(chunks))
	}
}

func TestChunkExchangesAICap(t *testing.T) {
	// Need >= 3 ">" markers to trigger exchange chunking.
	var lines []string
	lines = append(lines, "> First question about something important?")
	for i := 0; i < 15; i++ {
		lines = append(lines, "This is AI response line number that is long enough.")
	}
	lines = append(lines, "> Second question about another topic?")
	lines = append(lines, "Short answer.")
	lines = append(lines, "> Third question for marker count?")
	lines = append(lines, "Another short answer.")
	content := strings.Join(lines, "\n")
	chunks := ChunkExchanges(content)
	if len(chunks) == 0 {
		t.Fatal("expected at least 1 chunk")
	}
	// First chunk: AI response capped at 8 lines joined with space.
	aiPart := strings.Split(chunks[0].Content, "\n")
	if len(aiPart) > 2 { // user turn + joined AI response (max 8 lines joined as one)
		t.Errorf("AI response should be joined into one line, got %d lines", len(aiPart))
	}
}

func TestChunkExchangesMinSize(t *testing.T) {
	content := "> hi\nok"
	chunks := ChunkExchanges(content)
	// "hi\nok" is < MinChunkSize, with fewer than 3 markers falls to paragraph.
	// Paragraph also filters < MinChunkSize.
	for _, c := range chunks {
		if len(c.Content) < MinChunkSize {
			t.Errorf("chunk below MinChunkSize: %q", c.Content)
		}
	}
}

func TestDetectConvoRoom(t *testing.T) {
	content := "We talked about code and python and debugging the function and testing the api endpoint"
	room := DetectConvoRoom(content)
	if room != "technical" {
		t.Errorf("expected technical, got %s", room)
	}
}

func TestDetectConvoRoomFallback(t *testing.T) {
	content := "Just a random conversation about weather and food"
	room := DetectConvoRoom(content)
	if room != "general" {
		t.Errorf("expected general, got %s", room)
	}
}

func TestScanConvos(t *testing.T) {
	dir := t.TempDir()
	// Create files.
	for _, name := range []string{"chat.txt", "export.json", "notes.md", "code.py", "data.jsonl"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("content"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	files, err := ScanConvos(dir)
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, f := range files {
		found[filepath.Base(f)] = true
	}
	for _, want := range []string{"chat.txt", "export.json", "notes.md", "data.jsonl"} {
		if !found[want] {
			t.Errorf("missing %s in scan results", want)
		}
	}
	if found["code.py"] {
		t.Error(".py file should not be in convo scan results")
	}
}

func TestScanConvosSkipMetaJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chat.meta.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ScanConvos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected .meta.json to be skipped, got %v", files)
	}
}

func TestScanConvosSkipDirs(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDir, "config.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := ScanConvos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected .git contents to be skipped, got %v", files)
	}
}

func TestDetectConvoRoomArchitecture(t *testing.T) {
	content := "We discussed the architecture and design pattern for the component structure and module interface"
	room := DetectConvoRoom(content)
	if room != "architecture" {
		t.Errorf("expected architecture, got %s", room)
	}
}

func TestDetectConvoRoomPlanning(t *testing.T) {
	content := "We need to plan the roadmap for the next sprint and set milestone deadlines"
	room := DetectConvoRoom(content)
	if room != "planning" {
		t.Errorf("expected planning, got %s", room)
	}
}

func TestDetectConvoRoomDecisions(t *testing.T) {
	content := "We decided to switch and migrated to the new framework after we chose it and picked the alternative approach"
	room := DetectConvoRoom(content)
	if room != "decisions" {
		t.Errorf("expected decisions, got %s", room)
	}
}

func TestDetectConvoRoomProblems(t *testing.T) {
	content := "The problem was a crash that failed and we found a workaround to fix the broken issue and resolved the stuck situation"
	room := DetectConvoRoom(content)
	if room != "problems" {
		t.Errorf("expected problems, got %s", room)
	}
}

func TestChunkExchangesEmpty(t *testing.T) {
	chunks := ChunkExchanges("")
	if len(chunks) != 0 {
		t.Errorf("expected 0 chunks from empty content, got %d", len(chunks))
	}
}

func TestScanConvosEmptyDir(t *testing.T) {
	dir := t.TempDir()
	files, err := ScanConvos(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Errorf("expected 0 files in empty dir, got %d", len(files))
	}
}
