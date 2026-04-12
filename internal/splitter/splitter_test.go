package splitter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFindSessionBoundaries(t *testing.T) {
	lines := []string{
		"Claude Code v1.0 started\n",
		"some content\n",
		"more content\n",
		"Claude Code v1.0 started\n",
		"second session\n",
	}
	got := FindSessionBoundaries(lines)
	if len(got) != 2 {
		t.Fatalf("expected 2 boundaries, got %d", len(got))
	}
	if got[0] != 0 || got[1] != 3 {
		t.Errorf("boundaries = %v, want [0 3]", got)
	}
}

func TestIsTrueSessionStart_ContextRestore(t *testing.T) {
	lines := []string{
		"Claude Code v1.0 started\n",
		"Press Ctrl+E to show 5 previous messages\n",
		"stuff\n",
	}
	got := FindSessionBoundaries(lines)
	if len(got) != 0 {
		t.Errorf("context restore should not be a boundary, got %d", len(got))
	}
}

func TestExtractTimestamp(t *testing.T) {
	lines := []string{
		"Claude Code v1.0\n",
		"⏺ 3:45 PM Wednesday, March 30, 2026\n",
		"some content\n",
	}
	human, iso := ExtractTimestamp(lines)
	if human != "2026-03-30_345PM" {
		t.Errorf("human = %q, want 2026-03-30_345PM", human)
	}
	if iso != "2026-03-30" {
		t.Errorf("iso = %q, want 2026-03-30", iso)
	}
}

func TestExtractPeople(t *testing.T) {
	lines := []string{
		"Alice said something to Ben about the project\n",
	}
	people := ExtractPeople(lines, []string{"Alice", "Ben", "Riley"})
	if len(people) != 2 || people[0] != "Alice" || people[1] != "Ben" {
		t.Errorf("people = %v, want [Alice Ben]", people)
	}
}

func TestExtractSubject(t *testing.T) {
	lines := []string{
		"> cd /tmp\n",
		"> ls -la\n",
		"> Please help me fix the login page bug\n",
	}
	got := ExtractSubject(lines)
	if got == "session" {
		t.Errorf("expected a real subject, got 'session'")
	}
	if strings.Contains(got, " ") {
		t.Errorf("subject should be hyphenated, got %q", got)
	}
}

func TestExtractSubject_FallbackSession(t *testing.T) {
	lines := []string{
		"> cd /tmp\n",
		"> git status\n",
	}
	got := ExtractSubject(lines)
	if got != "session" {
		t.Errorf("expected 'session' for shell-only commands, got %q", got)
	}
}

func TestSplitFile(t *testing.T) {
	dir := t.TempDir()
	content := "Claude Code v1.0\nfirst session content line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\n" +
		"Claude Code v1.0\nsecond session content line 1\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\n"
	srcPath := filepath.Join(dir, "mega.txt")
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Split(dir, SplitOptions{MinSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].FilesWritten < 2 {
		t.Errorf("expected at least 2 files written, got %d", results[0].FilesWritten)
	}
	// Original should be renamed.
	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("original file should be renamed to .mega_backup")
	}
}

func TestSplitDryRun(t *testing.T) {
	dir := t.TempDir()
	content := "Claude Code v1.0\nfirst session content\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\n" +
		"Claude Code v1.0\nsecond session content\nline 2\nline 3\nline 4\nline 5\nline 6\nline 7\nline 8\nline 9\nline 10\nline 11\n"
	srcPath := filepath.Join(dir, "mega.txt")
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Split(dir, SplitOptions{DryRun: true, MinSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	// Original should NOT be renamed in dry-run.
	if _, err := os.Stat(srcPath); err != nil {
		t.Error("original file should still exist in dry-run")
	}
	// No output files should exist (only source).
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file in dir (original), got %d", len(entries))
	}
}
