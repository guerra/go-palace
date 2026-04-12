package normalize

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeReadsPlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got != "hello world\n" {
		t.Errorf("got %q, want %q", got, "hello world\n")
	}
}

func TestNormalizePreservesInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.bin")
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got len %d, want 3", len(got))
	}
}

func TestNormalizeRejectsHugeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	_, err = Normalize(path)
	if err == nil {
		t.Fatal("expected error on oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %v missing 'too large'", err)
	}
}

func TestNormalizeMissingFile(t *testing.T) {
	_, err := Normalize(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected error on missing file")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("error %v missing 'stat'", err)
	}
}

func TestNormalizeClaudeCodeJSONL(t *testing.T) {
	content := `{"type":"human","message":{"content":"What is Go?"}}
{"type":"assistant","message":{"content":"Go is a programming language."}}
{"type":"human","message":{"content":"Tell me more"}}
{"type":"assistant","message":{"content":"It was created by Google."}}`
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> What is Go?") {
		t.Errorf("missing user turn marker: %s", got)
	}
	if !strings.Contains(got, "Go is a programming language.") {
		t.Errorf("missing assistant response: %s", got)
	}
}

func TestNormalizeCodexJSONL(t *testing.T) {
	content := `{"type":"session_meta","session_id":"abc"}
{"type":"event_msg","payload":{"type":"user_message","message":"How do I deploy?"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Use docker compose."}}
{"type":"event_msg","payload":{"type":"user_message","message":"What about k8s?"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"That works too."}}`
	dir := t.TempDir()
	path := filepath.Join(dir, "rollout.jsonl")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> How do I deploy?") {
		t.Errorf("missing user turn: %s", got)
	}
	if !strings.Contains(got, "Use docker compose.") {
		t.Errorf("missing agent response: %s", got)
	}
}

func TestNormalizeClaudeAIJSON(t *testing.T) {
	content := `[
		{"role": "user", "content": "Hello Claude"},
		{"role": "assistant", "content": "Hello! How can I help?"},
		{"role": "user", "content": "Write a poem"},
		{"role": "assistant", "content": "Roses are red..."}
	]`
	dir := t.TempDir()
	path := filepath.Join(dir, "claude.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> Hello Claude") {
		t.Errorf("missing user turn: %s", got)
	}
	if !strings.Contains(got, "Hello! How can I help?") {
		t.Errorf("missing assistant response: %s", got)
	}
}

func TestNormalizeClaudeAIPrivacyJSON(t *testing.T) {
	content := `[
		{
			"chat_messages": [
				{"role": "user", "content": "What is AI?"},
				{"role": "assistant", "content": "AI is artificial intelligence."},
				{"role": "user", "content": "Cool"},
				{"role": "assistant", "content": "Indeed!"}
			]
		}
	]`
	dir := t.TempDir()
	path := filepath.Join(dir, "privacy.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> What is AI?") {
		t.Errorf("missing user turn: %s", got)
	}
}

func TestNormalizeChatGPTJSON(t *testing.T) {
	content := `{
		"mapping": {
			"root": {"parent": null, "message": null, "children": ["node1"]},
			"node1": {"parent": "root", "message": {"author": {"role": "user"}, "content": {"parts": ["What is Go?"]}}, "children": ["node2"]},
			"node2": {"parent": "node1", "message": {"author": {"role": "assistant"}, "content": {"parts": ["Go is a language."]}}, "children": ["node3"]},
			"node3": {"parent": "node2", "message": {"author": {"role": "user"}, "content": {"parts": ["Tell me more"]}}, "children": ["node4"]},
			"node4": {"parent": "node3", "message": {"author": {"role": "assistant"}, "content": {"parts": ["Created by Google."]}}, "children": []}
		}
	}`
	dir := t.TempDir()
	path := filepath.Join(dir, "chatgpt.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> What is Go?") {
		t.Errorf("missing user turn: %s", got)
	}
	if !strings.Contains(got, "Go is a language.") {
		t.Errorf("missing assistant response: %s", got)
	}
	if !strings.Contains(got, "> Tell me more") {
		t.Errorf("missing second user turn: %s", got)
	}
}

func TestNormalizeSlackJSON(t *testing.T) {
	content := `[
		{"type": "message", "user": "U001", "text": "Hey can you help?"},
		{"type": "message", "user": "U002", "text": "Sure, what do you need?"},
		{"type": "message", "user": "U001", "text": "How do I deploy?"},
		{"type": "message", "user": "U002", "text": "Just push to main."}
	]`
	dir := t.TempDir()
	path := filepath.Join(dir, "slack.json")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if !strings.Contains(got, "> Hey can you help?") {
		t.Errorf("missing first user turn: %s", got)
	}
	if !strings.Contains(got, "Sure, what do you need?") {
		t.Errorf("missing assistant response: %s", got)
	}
}

func TestNormalizePlainTextPassthrough(t *testing.T) {
	// Text with >= 3 ">" lines passes through unchanged.
	content := "> line one\nresponse\n> line two\nresponse\n> line three\nresponse\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "plain.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got != content {
		t.Errorf("expected passthrough for text with >= 3 markers, got %q", got)
	}
}

func TestNormalizePlainTextNoMarkers(t *testing.T) {
	content := "Just some regular text\nwith no special markers\nat all\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "regular.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got != content {
		t.Errorf("expected passthrough for regular text, got %q", got)
	}
}

// --- Normalize top-level edge cases ---

func TestNormalizeEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0o644)
	got, err := Normalize(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestNormalizeWhitespaceOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ws.txt")
	os.WriteFile(path, []byte("   \n  \n  "), 0o644)
	got, err := Normalize(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(got) != "" {
		t.Errorf("expected empty after trim, got %q", got)
	}
}

func TestNormalizeJSONContentDetectedByBrace(t *testing.T) {
	data := []map[string]string{
		{"role": "user", "content": "Hey"},
		{"role": "assistant", "content": "Hi there"},
	}
	b, _ := json.Marshal(data)
	dir := t.TempDir()
	path := filepath.Join(dir, "chat.txt") // .txt but starts with [
	os.WriteFile(path, b, 0o644)
	got, err := Normalize(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Hey") {
		t.Errorf("expected JSON detection for .txt starting with [, got %q", got)
	}
}

// --- extractContent tests ---

func TestExtractContentString(t *testing.T) {
	if got := extractContent("hello"); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestExtractContentListOfStrings(t *testing.T) {
	input := []any{"hello", "world"}
	if got := extractContent(input); got != "hello world" {
		t.Errorf("got %q, want 'hello world'", got)
	}
}

func TestExtractContentListOfBlocks(t *testing.T) {
	input := []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "image", "url": "x"},
	}
	if got := extractContent(input); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestExtractContentDict(t *testing.T) {
	input := map[string]any{"text": "hello"}
	if got := extractContent(input); got != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestExtractContentNil(t *testing.T) {
	if got := extractContent(nil); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestExtractContentMixedList(t *testing.T) {
	input := []any{"plain", map[string]any{"type": "text", "text": "block"}}
	if got := extractContent(input); got != "plain block" {
		t.Errorf("got %q, want 'plain block'", got)
	}
}

// --- tryClaudeCodeJSONL edge cases ---

func TestClaudeCodeJSONLUserType(t *testing.T) {
	lines := []string{
		`{"type":"user","message":{"content":"Q"}}`,
		`{"type":"assistant","message":{"content":"A"}}`,
	}
	result, ok := tryClaudeCodeJSONL(strings.Join(lines, "\n"))
	if !ok || result == "" {
		t.Fatal("expected result")
	}
	if !strings.Contains(result, "> Q") {
		t.Errorf("missing user turn: %s", result)
	}
}

func TestClaudeCodeJSONLTooFewMessages(t *testing.T) {
	lines := []string{`{"type":"human","message":{"content":"only one"}}`}
	_, ok := tryClaudeCodeJSONL(strings.Join(lines, "\n"))
	if ok {
		t.Error("expected false for single message")
	}
}

func TestClaudeCodeJSONLInvalidJSONLines(t *testing.T) {
	lines := []string{
		"not json",
		`{"type":"human","message":{"content":"Q"}}`,
		`{"type":"assistant","message":{"content":"A"}}`,
	}
	result, ok := tryClaudeCodeJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Fatal("expected ok — bad lines should be skipped")
	}
	if !strings.Contains(result, "> Q") {
		t.Errorf("missing user turn: %s", result)
	}
}

func TestClaudeCodeJSONLNonDictEntries(t *testing.T) {
	lines := []string{
		`[1, 2, 3]`,
		`{"type":"human","message":{"content":"Q"}}`,
		`{"type":"assistant","message":{"content":"A"}}`,
	}
	result, ok := tryClaudeCodeJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Fatal("expected ok — non-dict entries should be skipped")
	}
	if !strings.Contains(result, "> Q") {
		t.Errorf("missing user turn: %s", result)
	}
	if strings.Contains(result, "[1, 2, 3]") {
		t.Error("non-dict array content should not appear in transcript")
	}
}

// --- tryCodexJSONL edge cases ---

func TestCodexJSONLNoSessionMeta(t *testing.T) {
	lines := []string{
		`{"type":"event_msg","payload":{"type":"user_message","message":"Q"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"A"}}`,
	}
	_, ok := tryCodexJSONL(strings.Join(lines, "\n"))
	if ok {
		t.Error("expected false without session_meta")
	}
}

func TestCodexJSONLSkipsNonEventMsg(t *testing.T) {
	lines := []string{
		`{"type":"session_meta"}`,
		`{"type":"response_item","payload":{"type":"user_message","message":"X"}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"Q"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"A"}}`,
	}
	result, ok := tryCodexJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Fatal("expected ok")
	}
	// X should not appear before Q
	idx := strings.Index(result, "> Q")
	if idx < 0 {
		t.Fatal("missing > Q")
	}
	before := result[:idx]
	if strings.Contains(before, "X") {
		t.Error("response_item should not produce output")
	}
}

func TestCodexJSONLNonStringMessage(t *testing.T) {
	lines := []string{
		`{"type":"session_meta"}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":123}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"Q"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"A"}}`,
	}
	_, ok := tryCodexJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Error("expected ok — non-string message skipped")
	}
}

func TestCodexJSONLEmptyTextSkipped(t *testing.T) {
	lines := []string{
		`{"type":"session_meta"}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"  "}}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"Q"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"A"}}`,
	}
	_, ok := tryCodexJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Error("expected ok — whitespace-only message skipped")
	}
}

func TestCodexJSONLPayloadNotDict(t *testing.T) {
	lines := []string{
		`{"type":"session_meta"}`,
		`{"type":"event_msg","payload":"not a dict"}`,
		`{"type":"event_msg","payload":{"type":"user_message","message":"Q"}}`,
		`{"type":"event_msg","payload":{"type":"agent_message","message":"A"}}`,
	}
	_, ok := tryCodexJSONL(strings.Join(lines, "\n"))
	if !ok {
		t.Error("expected ok — non-dict payload skipped")
	}
}

// --- tryClaudeAIJSON edge cases ---

func TestClaudeAIDictWithMessagesKey(t *testing.T) {
	data := map[string]any{
		"messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{"role": "assistant", "content": "Hi"},
		},
	}
	result, ok := tryClaudeAIJSON(data)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(result, "> Hello") {
		t.Error("missing user turn")
	}
}

func TestClaudeAINotAList(t *testing.T) {
	_, ok := tryClaudeAIJSON("not a list")
	if ok {
		t.Error("expected false for non-list")
	}
}

func TestClaudeAITooFewMessages(t *testing.T) {
	data := []any{map[string]any{"role": "user", "content": "Hello"}}
	_, ok := tryClaudeAIJSON(data)
	if ok {
		t.Error("expected false for single message")
	}
}

func TestClaudeAIDictWithChatMessagesKey(t *testing.T) {
	data := map[string]any{
		"chat_messages": []any{
			map[string]any{"role": "user", "content": "Hello"},
			map[string]any{"role": "assistant", "content": "World"},
		},
	}
	result, ok := tryClaudeAIJSON(data)
	if !ok {
		t.Fatal("expected ok")
	}
	if !strings.Contains(result, "> Hello") {
		t.Errorf("missing user turn: %s", result)
	}
}

func TestClaudeAIPrivacyExportNonDictItems(t *testing.T) {
	data := []any{
		map[string]any{
			"chat_messages": []any{
				"not a dict",
				map[string]any{"role": "user", "content": "Q"},
				map[string]any{"role": "assistant", "content": "A"},
			},
		},
		"not a convo",
	}
	result, ok := tryClaudeAIJSON(data)
	if !ok {
		t.Fatal("expected ok — non-dict items skipped")
	}
	if !strings.Contains(result, "> Q") {
		t.Errorf("missing user turn: %s", result)
	}
	if !strings.Contains(result, "A") {
		t.Errorf("missing assistant turn: %s", result)
	}
}

// --- tryChatGPTJSON edge cases ---

func TestChatGPTJSONNoMapping(t *testing.T) {
	_, ok := tryChatGPTJSON(map[string]any{"data": []any{}})
	if ok {
		t.Error("expected false for no mapping key")
	}
}

func TestChatGPTJSONNotDict(t *testing.T) {
	_, ok := tryChatGPTJSON([]any{1, 2, 3})
	if ok {
		t.Error("expected false for non-dict")
	}
}

func TestChatGPTJSONFallbackRoot(t *testing.T) {
	data := map[string]any{
		"mapping": map[string]any{
			"root": map[string]any{
				"parent": nil,
				"message": map[string]any{
					"author":  map[string]any{"role": "system"},
					"content": map[string]any{"parts": []any{"system prompt"}},
				},
				"children": []any{"msg1"},
			},
			"msg1": map[string]any{
				"parent": "root",
				"message": map[string]any{
					"author":  map[string]any{"role": "user"},
					"content": map[string]any{"parts": []any{"Hello"}},
				},
				"children": []any{"msg2"},
			},
			"msg2": map[string]any{
				"parent": "msg1",
				"message": map[string]any{
					"author":  map[string]any{"role": "assistant"},
					"content": map[string]any{"parts": []any{"Hi there"}},
				},
				"children": []any{},
			},
		},
	}
	_, ok := tryChatGPTJSON(data)
	if !ok {
		t.Error("expected ok with fallback root")
	}
}

func TestChatGPTJSONTooFewMessages(t *testing.T) {
	data := map[string]any{
		"mapping": map[string]any{
			"root": map[string]any{
				"parent": nil, "message": nil, "children": []any{"msg1"},
			},
			"msg1": map[string]any{
				"parent": "root",
				"message": map[string]any{
					"author":  map[string]any{"role": "user"},
					"content": map[string]any{"parts": []any{"Only one"}},
				},
				"children": []any{},
			},
		},
	}
	_, ok := tryChatGPTJSON(data)
	if ok {
		t.Error("expected false for single message")
	}
}

// --- trySlackJSON edge cases ---

func TestSlackJSONNotAList(t *testing.T) {
	_, ok := trySlackJSON(map[string]any{"type": "message"})
	if ok {
		t.Error("expected false for non-list")
	}
}

func TestSlackJSONTooFewMessages(t *testing.T) {
	data := []any{map[string]any{"type": "message", "user": "U1", "text": "Hello"}}
	_, ok := trySlackJSON(data)
	if ok {
		t.Error("expected false for single message")
	}
}

func TestSlackJSONSkipsNonMessageTypes(t *testing.T) {
	data := []any{
		map[string]any{"type": "channel_join", "user": "U1", "text": "joined"},
		map[string]any{"type": "message", "user": "U1", "text": "Hello"},
		map[string]any{"type": "message", "user": "U2", "text": "Hi"},
	}
	result, ok := trySlackJSON(data)
	if !ok {
		t.Fatal("expected ok")
	}
	if strings.Contains(result, "joined") {
		t.Error("channel_join text should not appear")
	}
}

func TestSlackJSONThreeUsers(t *testing.T) {
	data := []any{
		map[string]any{"type": "message", "user": "U1", "text": "Hello"},
		map[string]any{"type": "message", "user": "U2", "text": "Hi"},
		map[string]any{"type": "message", "user": "U3", "text": "Hey"},
	}
	result, ok := trySlackJSON(data)
	if !ok {
		t.Fatal("expected ok with 3 users")
	}
	if !strings.Contains(result, "Hello") {
		t.Error("missing U1 text")
	}
	if !strings.Contains(result, "Hi") {
		t.Error("missing U2 text")
	}
	if !strings.Contains(result, "Hey") {
		t.Error("missing U3 text")
	}
}

func TestSlackJSONEmptyTextSkipped(t *testing.T) {
	data := []any{
		map[string]any{"type": "message", "user": "U1", "text": ""},
		map[string]any{"type": "message", "user": "U1", "text": "Hello"},
		map[string]any{"type": "message", "user": "U2", "text": "Hi"},
	}
	_, ok := trySlackJSON(data)
	if !ok {
		t.Error("expected ok — empty text skipped")
	}
}

func TestSlackJSONUsernameFallback(t *testing.T) {
	data := []any{
		map[string]any{"type": "message", "username": "bot1", "text": "Hello"},
		map[string]any{"type": "message", "username": "bot2", "text": "Hi"},
	}
	_, ok := trySlackJSON(data)
	if !ok {
		t.Error("expected ok with username fallback")
	}
}

// --- tryNormalizeJSON edge cases ---

func TestTryNormalizeJSONInvalidJSON(t *testing.T) {
	_, ok := tryNormalizeJSON("not json at all {{{")
	if ok {
		t.Error("expected false for invalid JSON")
	}
}

func TestTryNormalizeJSONValidButUnknownSchema(t *testing.T) {
	_, ok := tryNormalizeJSON(`{"random": "data"}`)
	if ok {
		t.Error("expected false for unknown schema")
	}
}

// --- messagesToTranscript edge cases ---

func TestMessagesToTranscriptConsecutiveUsers(t *testing.T) {
	msgs := []messagePair{
		{Role: "user", Text: "Q1"},
		{Role: "user", Text: "Q2"},
		{Role: "assistant", Text: "A"},
	}
	result := messagesToTranscript(msgs, false)
	if !strings.Contains(result, "> Q1") {
		t.Error("missing > Q1")
	}
	if !strings.Contains(result, "> Q2") {
		t.Error("missing > Q2")
	}
}

func TestMessagesToTranscriptAssistantFirst(t *testing.T) {
	msgs := []messagePair{
		{Role: "assistant", Text: "preamble"},
		{Role: "user", Text: "Q"},
		{Role: "assistant", Text: "A"},
	}
	result := messagesToTranscript(msgs, false)
	if !strings.Contains(result, "preamble") {
		t.Error("missing preamble")
	}
	if !strings.Contains(result, "> Q") {
		t.Error("missing > Q")
	}
}
