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

func TestSplitFile_NotMega(t *testing.T) {
	dir := t.TempDir()
	content := "Claude Code v1.0\nJust one session\n" + strings.Repeat("line\n", 20)
	srcPath := filepath.Join(dir, "single.txt")
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Split(dir, SplitOptions{MinSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("single-session file should not produce results, got %+v", results)
	}
}

func TestSplitFile_OutputDirNone(t *testing.T) {
	dir := t.TempDir()
	content := "Claude Code v1.0\nfirst session content\n" + strings.Repeat("line\n", 10) +
		"Claude Code v1.0\nsecond session content\n" + strings.Repeat("line\n", 10)
	srcPath := filepath.Join(dir, "mega.txt")
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	// OutputDir empty means write to same dir as source.
	results, err := Split(dir, SplitOptions{MinSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	for _, p := range results[0].OutputPaths {
		if filepath.Dir(p) != dir {
			t.Errorf("expected output in %s, got %s", dir, filepath.Dir(p))
		}
	}
}

func TestSplitFile_TinyFragmentsSkipped(t *testing.T) {
	dir := t.TempDir()
	// First session: only 3 lines (< 10), should be skipped.
	// Second session: 15 lines, should be kept.
	content := "Claude Code v1.0\nline1\nline2\n" +
		"Claude Code v1.0\nsecond session content\n" + strings.Repeat("line\n", 14)
	srcPath := filepath.Join(dir, "tiny.txt")
	if err := os.WriteFile(srcPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	results, err := Split(dir, SplitOptions{MinSessions: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Fatal("expected results")
	}
	// The first tiny chunk (< 10 lines) should be skipped.
	if results[0].FilesWritten != 1 {
		t.Errorf("expected 1 file written (tiny skipped), got %d", results[0].FilesWritten)
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

// --- isTrueSessionStart ---

func TestIsTrueSessionStart_Yes(t *testing.T) {
	lines := []string{
		"Claude Code v1.0\n",
		"Some content\n",
		"More content\n",
		"\n",
		"\n",
		"\n",
	}
	boundaries := FindSessionBoundaries(lines)
	if len(boundaries) != 1 || boundaries[0] != 0 {
		t.Errorf("expected boundary at 0, got %v", boundaries)
	}
}

func TestIsTrueSessionStart_NoPreviousMessages(t *testing.T) {
	lines := []string{
		"Claude Code v1.0\n",
		"Some text\n",
		"previous messages here\n",
		"\n",
		"\n",
		"\n",
	}
	boundaries := FindSessionBoundaries(lines)
	if len(boundaries) != 0 {
		t.Errorf("'previous messages' should disqualify, got %v", boundaries)
	}
}

// --- FindSessionBoundaries ---

func TestFindSessionBoundaries_None(t *testing.T) {
	lines := []string{"Just some text\n", "No sessions here\n"}
	boundaries := FindSessionBoundaries(lines)
	if len(boundaries) != 0 {
		t.Errorf("expected 0 boundaries, got %v", boundaries)
	}
}

// --- ExtractTimestamp ---

func TestExtractTimestamp_NotFound(t *testing.T) {
	lines := []string{"No timestamp here\n"}
	human, iso := ExtractTimestamp(lines)
	if human != "" || iso != "" {
		t.Errorf("expected empty, got human=%q iso=%q", human, iso)
	}
}

func TestExtractTimestamp_OnlyChecksFirst50(t *testing.T) {
	lines := make([]string, 52)
	for i := 0; i < 51; i++ {
		lines[i] = "filler\n"
	}
	lines[51] = "⏺ 1:00 AM Monday, January 01, 2026\n"
	human, _ := ExtractTimestamp(lines)
	if human != "" {
		t.Errorf("timestamp beyond line 50 should not be found, got %q", human)
	}
}

// --- ExtractSubject ---

func TestExtractSubject_Found(t *testing.T) {
	lines := []string{"> How do we handle authentication?\n"}
	subject := ExtractSubject(lines)
	if !strings.Contains(strings.ToLower(subject), "authentication") {
		t.Errorf("expected subject containing 'authentication', got %q", subject)
	}
}

func TestExtractSubject_ShortPromptSkipped(t *testing.T) {
	lines := []string{"> ok\n", "> yes\n", "> What about the deployment strategy?\n"}
	subject := ExtractSubject(lines)
	if !strings.Contains(strings.ToLower(subject), "deployment") {
		t.Errorf("expected subject containing 'deployment', got %q", subject)
	}
}

func TestExtractSubject_Truncated(t *testing.T) {
	lines := []string{"> " + strings.Repeat("a", 100) + "\n"}
	subject := ExtractSubject(lines)
	if len(subject) > 60 {
		t.Errorf("subject should be <= 60 chars, got %d", len(subject))
	}
}

// --- LoadKnownPeople / Config ---

func TestLoadKnownPeople_FallsBack(t *testing.T) {
	// With no config file, should return default people.
	old := knownNamesPath
	knownNamesPath = filepath.Join(t.TempDir(), "missing.json")
	defer func() { knownNamesPath = old }()

	people := loadKnownPeople()
	if len(people) == 0 {
		t.Error("expected fallback known people, got empty")
	}
	if people[0] != defaultKnownPeople[0] {
		t.Errorf("expected first person %q, got %q", defaultKnownPeople[0], people[0])
	}
}

func TestLoadKnownPeople_FromList(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "known_names.json")
	if err := os.WriteFile(configPath, []byte(`["Alice","Ben"]`), 0o644); err != nil {
		t.Fatal(err)
	}

	old := knownNamesPath
	knownNamesPath = configPath
	defer func() { knownNamesPath = old }()

	people := loadKnownPeople()
	if len(people) != 2 || people[0] != "Alice" || people[1] != "Ben" {
		t.Errorf("expected [Alice Ben], got %v", people)
	}
}

func TestLoadKnownPeople_FromDict(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "known_names.json")
	if err := os.WriteFile(configPath, []byte(`{"names":["Alice"],"username_map":{"jdoe":"John"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	old := knownNamesPath
	knownNamesPath = configPath
	defer func() { knownNamesPath = old }()

	people := loadKnownPeople()
	if len(people) != 1 || people[0] != "Alice" {
		t.Errorf("expected [Alice], got %v", people)
	}
}

func TestExtractPeopleUsesUsernameMap(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "known_names.json")
	if err := os.WriteFile(configPath, []byte(`{"names":["Alice"],"username_map":{"jdoe":"John"}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	old := knownNamesPath
	knownNamesPath = configPath
	defer func() { knownNamesPath = old }()

	lines := []string{"Working in /Users/jdoe/project\n"}
	people := ExtractPeople(lines, []string{"Alice"})
	found := map[string]bool{}
	for _, p := range people {
		found[p] = true
	}
	if !found["John"] {
		t.Errorf("expected John from username_map, got %v", people)
	}
}

func TestExtractPeopleDetectsNames(t *testing.T) {
	lines := []string{"> Alice reviewed the change with Ben\n"}
	people := ExtractPeople(lines, []string{"Alice", "Ben"})
	if len(people) != 2 || people[0] != "Alice" || people[1] != "Ben" {
		t.Errorf("expected [Alice Ben], got %v", people)
	}
}

func TestLoadKnownNamesConfig_InvalidJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "known_names.json")
	if err := os.WriteFile(configPath, []byte("not json {{{"), 0o644); err != nil {
		t.Fatal(err)
	}

	old := knownNamesPath
	knownNamesPath = configPath
	defer func() { knownNamesPath = old }()

	// Invalid JSON should fall back to defaults.
	people := loadKnownPeople()
	if len(people) == 0 {
		t.Error("expected fallback known people")
	}
}
