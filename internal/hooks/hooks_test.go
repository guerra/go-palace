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

// --- countHumanMessages additional tests ---

func TestCountHumanMessages_ListContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	lines := []string{
		`{"message":{"role":"user","content":[{"type":"text","text":"hello"}]}}`,
		`{"message":{"role":"user","content":[{"type":"text","text":"<command-message>x</command-message>"}]}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	got := countHumanMessages(path)
	if got != 1 {
		t.Errorf("countHumanMessages with list content = %d, want 1", got)
	}
}

func TestCountHumanMessages_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	got := countHumanMessages(path)
	if got != 0 {
		t.Errorf("countHumanMessages empty file = %d, want 0", got)
	}
}

func TestCountHumanMessages_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "transcript.jsonl")
	content := "not json\n" + `{"message":{"role":"user","content":"ok"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got := countHumanMessages(path)
	if got != 1 {
		t.Errorf("countHumanMessages malformed = %d, want 1", got)
	}
}

func TestCountHumanMessages_MissingFile(t *testing.T) {
	got := countHumanMessages("/nonexistent/path.jsonl")
	if got != 0 {
		t.Errorf("countHumanMessages missing file = %d, want 0", got)
	}
}

// --- hookStop additional tests ---

func TestHookStop_PassthroughWhenActive_Bool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	input, _ := json.Marshal(map[string]any{
		"session_id":       "test",
		"stop_hook_active": true,
		"transcript_path":  "",
	})
	var out bytes.Buffer
	if err := RunHook("stop", "claude-code", bytes.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "" {
		t.Errorf("stop should pass through when active, got decision=%q", result.Decision)
	}
}

func TestHookStop_PassthroughWhenActive_String(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	// stop_hook_active as string "true"
	input := `{"session_id":"test","stop_hook_active":"true","transcript_path":""}`
	var out bytes.Buffer
	if err := RunHook("stop", "claude-code", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "" {
		t.Errorf("stop should pass through when active string, got decision=%q", result.Decision)
	}
}

func TestHookStop_TracksSavePoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < SaveInterval; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "save_track",
		"transcript_path":  transcript,
		"stop_hook_active": false,
	})

	// First call should block
	var out1 bytes.Buffer
	if err := RunHook("stop", "claude-code", bytes.NewReader(input), &out1); err != nil {
		t.Fatal(err)
	}
	var r1 HookOutput
	if err := json.Unmarshal(out1.Bytes(), &r1); err != nil {
		t.Fatal(err)
	}
	if r1.Decision != "block" {
		t.Fatalf("first call should block, got %q", r1.Decision)
	}

	// Second call with same count should pass through (already saved)
	var out2 bytes.Buffer
	if err := RunHook("stop", "claude-code", bytes.NewReader(input), &out2); err != nil {
		t.Fatal(err)
	}
	var r2 HookOutput
	if err := json.Unmarshal(out2.Bytes(), &r2); err != nil {
		t.Fatal(err)
	}
	if r2.Decision != "" {
		t.Errorf("second call should pass through, got decision=%q", r2.Decision)
	}
}

func TestHookStop_InvalidLastSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < SaveInterval; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write invalid content to last save file
	lastSave := filepath.Join(dir, "inv_test_last_save")
	if err := os.WriteFile(lastSave, []byte("not_a_number"), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "inv_test",
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
		t.Errorf("invalid last_save should fallback to 0, got decision=%q", result.Decision)
	}
}

func TestHookStop_BelowInterval(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < SaveInterval-1; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "below",
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
	if result.Decision != "" {
		t.Errorf("below interval should not block, got decision=%q", result.Decision)
	}
}

// --- isStopHookActive tests ---

func TestIsStopHookActive(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"true_bool", "true", true},
		{"false_bool", "false", false},
		{"true_string", `"true"`, true},
		{"false_string", `"false"`, false},
		{"one_string", `"1"`, true},
		{"yes_string", `"yes"`, true},
		{"null", "null", false},
		{"zero_number", "0", false},
		{"empty", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isStopHookActive(json.RawMessage(tt.raw))
			if got != tt.want {
				t.Errorf("isStopHookActive(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

// --- logEntry tests ---

func TestLogEntry_WritesToFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	logEntry("test log message")

	logPath := filepath.Join(dir, "hook.log")
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read hook.log: %v", err)
	}
	if !strings.Contains(string(data), "test log message") {
		t.Errorf("hook.log missing message: %s", string(data))
	}
}

// --- runHook dispatch + invalid JSON ---

func TestRunHook_InvalidJSON(t *testing.T) {
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", t.TempDir())
	var out bytes.Buffer
	// Invalid JSON should not error — falls back to empty input.
	if err := RunHook("session-start", "claude-code", strings.NewReader("not valid json"), &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Decision != "" {
		t.Errorf("invalid JSON session-start should produce empty decision, got %q", result.Decision)
	}
}

func TestPrecompact_VerifyReason(t *testing.T) {
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", t.TempDir())
	input := `{"session_id":"test"}`
	var out bytes.Buffer
	if err := RunHook("precompact", "claude-code", strings.NewReader(input), &out); err != nil {
		t.Fatal(err)
	}
	var result HookOutput
	if err := json.Unmarshal(out.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.Reason != PrecompactBlockReason {
		t.Errorf("precompact reason mismatch:\n  got:  %q\n  want: %q", result.Reason, PrecompactBlockReason)
	}
}

func TestHookStop_BlocksAtInterval(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_HOOK_STATE_DIR", dir)

	transcript := filepath.Join(dir, "transcript.jsonl")
	var lines []string
	for i := 0; i < SaveInterval; i++ {
		lines = append(lines, `{"message":{"role":"user","content":"hello"}}`)
	}
	if err := os.WriteFile(transcript, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}

	input, _ := json.Marshal(map[string]any{
		"session_id":       "interval_test",
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
		t.Errorf("should block at interval, got %q", result.Decision)
	}
	if result.Reason != StopBlockReason {
		t.Errorf("stop reason mismatch:\n  got:  %q\n  want: %q", result.Reason, StopBlockReason)
	}
}
