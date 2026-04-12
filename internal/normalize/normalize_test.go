package normalize

import (
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
