package hooks

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHookSessionStart(t *testing.T) {
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", t.TempDir())
	input := `{"session_id":"test123"}`
	var out bytes.Buffer
	if err := RunHook("session-start", "claude-code", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal output: %v (raw=%q)", err, out.String())
	}
	if result.Decision != "" {
		t.Errorf("session-start should not block, got decision=%q", result.Decision)
	}
}

func TestHookPrecompact(t *testing.T) {
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", t.TempDir())
	input := `{"session_id":"test123"}`
	var out bytes.Buffer
	if err := RunHook("precompact", "claude-code", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "block" {
		t.Errorf("precompact should block, got decision=%q", result.Decision)
	}
	if result.Reason == "" {
		t.Error("precompact should have a reason")
	}
}

func TestHookStop_NoBlock(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	// Create a transcript with just 3 human messages.
	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < 3; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "sess1",
		"transcript_path":  transcript,
		"stop_hook_active": false,
	})

	var out bytes.Buffer
	if err := RunHook("stop", "claude-code", bytes.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision == "block" {
		t.Error("stop should not block with only 3 messages")
	}
}

func TestHookStop_Block(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	// Create a transcript with 20 human messages.
	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "sess2",
		"transcript_path":  transcript,
		"stop_hook_active": false,
	})

	var out bytes.Buffer
	if err := RunHook("stop", "claude-code", bytes.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "block" {
		t.Errorf("stop should block with 20 messages, got decision=%q", result.Decision)
	}
}

func TestCountHumanMessages(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	lines := []string{
		`{"message":{"role":"user","content":"hello"}}`,
		`{"message":{"role":"assistant","content":"hi"}}`,
		`{"message":{"role":"user","content":"<command-message>cmd</command-message>"}}`,
		`{"message":{"role":"user","content":"real message"}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got := countHumanMessages(path)
	// 2 real user messages (first and last), 1 command-message skipped.
	if got != 2 {
		t.Errorf("countHumanMessages = %d, want 2", got)
	}
}

func TestSanitizeSessionID(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"abc-123_def", "abc-123_def"},
		{"../../../etc/passwd", "etcpasswd"},
		{"", "unknown"},
		{"hello world!", "helloworld"},
	}
	for _, tt := range tests {
		got := sanitizeSessionID(tt.input)
		if got != tt.want {
			t.Errorf("sanitizeSessionID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestUnknownHarness(t *testing.T) {
	err := RunHook("stop", "unknown-harness", strings.NewReader("{}"), &bytes.Buffer{})
	if err == nil {
		t.Error("expected error for unknown harness")
	}
}

func TestUnknownHook(t *testing.T) {
	err := RunHook("nonexistent", "claude-code", strings.NewReader("{}"), &bytes.Buffer{})
	if err == nil {
		t.Error("expected error for unknown hook")
	}
}
