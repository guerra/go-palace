// Package normalize converts raw source files into plain text that the
// miner can chunk and embed. Phase C implements full format detection:
// Claude Code JSONL, Codex JSONL, Claude.ai JSON, ChatGPT JSON, Slack JSON,
// and plain text pass-through.
package normalize

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/guerra/go-palace/internal/spellcheck"
)

// MaxFileSize is the safety cap borrowed from mempalace/normalize.py:32 —
// any file larger than 500 MB is rejected outright to protect the miner
// from accidental huge inputs.
const MaxFileSize int64 = 500 * 1024 * 1024

// messagePair holds a single turn in the conversation.
type messagePair struct {
	Role string
	Text string
}

// Normalize loads path and returns its contents as a string, applying
// format detection and normalization for known chat export formats.
// Port of normalize.py:23-55.
func Normalize(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("normalize: stat %s: %w", path, err)
	}
	if info.Size() > MaxFileSize {
		return "", fmt.Errorf("normalize: file too large (%d MB): %s",
			info.Size()/(1024*1024), path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("normalize: read %s: %w", path, err)
	}
	content := string(data)

	if strings.TrimSpace(content) == "" {
		return content, nil
	}

	// Already has >= 3 ">" markers — pass through (normalize.py:44-46).
	lines := strings.Split(content, "\n")
	quoteCount := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			quoteCount++
		}
	}
	if quoteCount >= 3 {
		return content, nil
	}

	// Try JSON normalization if file looks like JSON/JSONL (normalize.py:49-53).
	ext := strings.ToLower(filepath.Ext(path))
	trimmed := strings.TrimSpace(content)
	if ext == ".json" || ext == ".jsonl" || (len(trimmed) > 0 && (trimmed[0] == '{' || trimmed[0] == '[')) {
		if normalized, ok := tryNormalizeJSON(content); ok {
			return normalized, nil
		}
	}

	return content, nil
}

// tryNormalizeJSON tries all known JSON chat schemas.
// Port of _try_normalize_json in normalize.py:58-79.
func tryNormalizeJSON(content string) (string, bool) {
	if normalized, ok := tryClaudeCodeJSONL(content); ok {
		return normalized, true
	}

	if normalized, ok := tryCodexJSONL(content); ok {
		return normalized, true
	}

	// Full JSON parse
	var data any
	if err := json.Unmarshal([]byte(content), &data); err != nil {
		return "", false
	}

	if normalized, ok := tryClaudeAIJSON(data); ok {
		return normalized, true
	}
	if normalized, ok := tryChatGPTJSON(data); ok {
		return normalized, true
	}
	if normalized, ok := trySlackJSON(data); ok {
		return normalized, true
	}

	return "", false
}

// tryClaudeCodeJSONL parses Claude Code JSONL sessions.
// Port of _try_claude_code_jsonl in normalize.py:82-105.
func tryClaudeCodeJSONL(content string) (string, bool) {
	rawLines := strings.Split(strings.TrimSpace(content), "\n")
	var messages []messagePair
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		msgType, _ := entry["type"].(string)
		message, _ := entry["message"].(map[string]any)
		if message == nil {
			continue
		}
		if msgType == "human" || msgType == "user" {
			text := extractContent(message["content"])
			if text != "" {
				messages = append(messages, messagePair{Role: "user", Text: text})
			}
		} else if msgType == "assistant" {
			text := extractContent(message["content"])
			if text != "" {
				messages = append(messages, messagePair{Role: "assistant", Text: text})
			}
		}
	}
	if len(messages) >= 2 {
		return messagesToTranscript(messages, true), true
	}
	return "", false
}

// tryCodexJSONL parses OpenAI Codex CLI sessions.
// Port of _try_codex_jsonl in normalize.py:108-153.
func tryCodexJSONL(content string) (string, bool) {
	rawLines := strings.Split(strings.TrimSpace(content), "\n")
	var messages []messagePair
	hasSessionMeta := false
	for _, line := range rawLines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			continue
		}
		entryType, _ := entry["type"].(string)
		if entryType == "session_meta" {
			hasSessionMeta = true
			continue
		}
		if entryType != "event_msg" {
			continue
		}
		payload, _ := entry["payload"].(map[string]any)
		if payload == nil {
			continue
		}
		payloadType, _ := payload["type"].(string)
		msg, _ := payload["message"].(string)
		text := strings.TrimSpace(msg)
		if text == "" {
			continue
		}
		if payloadType == "user_message" {
			messages = append(messages, messagePair{Role: "user", Text: text})
		} else if payloadType == "agent_message" {
			messages = append(messages, messagePair{Role: "assistant", Text: text})
		}
	}
	if len(messages) >= 2 && hasSessionMeta {
		return messagesToTranscript(messages, true), true
	}
	return "", false
}

// tryClaudeAIJSON parses Claude.ai JSON export: flat messages list or privacy
// export with chat_messages. Port of _try_claude_ai_json in normalize.py:156-196.
func tryClaudeAIJSON(data any) (string, bool) {
	var list []any

	switch v := data.(type) {
	case map[string]any:
		if msgs, ok := v["messages"].([]any); ok {
			list = msgs
		} else if msgs, ok := v["chat_messages"].([]any); ok {
			list = msgs
		}
	case []any:
		list = v
	}

	if list == nil {
		return "", false
	}

	// Privacy export: array of conversation objects with chat_messages.
	if len(list) > 0 {
		if first, ok := list[0].(map[string]any); ok {
			if _, hasChatMsgs := first["chat_messages"]; hasChatMsgs {
				var allMessages []messagePair
				for _, item := range list {
					convo, ok := item.(map[string]any)
					if !ok {
						continue
					}
					chatMsgs, _ := convo["chat_messages"].([]any)
					for _, m := range chatMsgs {
						msg, ok := m.(map[string]any)
						if !ok {
							continue
						}
						role, _ := msg["role"].(string)
						text := extractContent(msg["content"])
						if (role == "user" || role == "human") && text != "" {
							allMessages = append(allMessages, messagePair{Role: "user", Text: text})
						} else if (role == "assistant" || role == "ai") && text != "" {
							allMessages = append(allMessages, messagePair{Role: "assistant", Text: text})
						}
					}
				}
				if len(allMessages) >= 2 {
					return messagesToTranscript(allMessages, true), true
				}
				return "", false
			}
		}
	}

	// Flat messages list.
	var messages []messagePair
	for _, item := range list {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		text := extractContent(msg["content"])
		if (role == "user" || role == "human") && text != "" {
			messages = append(messages, messagePair{Role: "user", Text: text})
		} else if (role == "assistant" || role == "ai") && text != "" {
			messages = append(messages, messagePair{Role: "assistant", Text: text})
		}
	}
	if len(messages) >= 2 {
		return messagesToTranscript(messages, true), true
	}
	return "", false
}

// tryChatGPTJSON parses ChatGPT conversations.json with mapping tree.
// Port of _try_chatgpt_json in normalize.py:199-237.
func tryChatGPTJSON(data any) (string, bool) {
	obj, ok := data.(map[string]any)
	if !ok {
		return "", false
	}
	mappingRaw, ok := obj["mapping"]
	if !ok {
		return "", false
	}
	mapping, ok := mappingRaw.(map[string]any)
	if !ok {
		return "", false
	}

	// Find root: prefer node with parent=nil AND no message.
	var rootID string
	var fallbackRoot string
	for nodeID, nodeRaw := range mapping {
		node, ok := nodeRaw.(map[string]any)
		if !ok {
			continue
		}
		if node["parent"] == nil {
			if node["message"] == nil {
				rootID = nodeID
				break
			} else if fallbackRoot == "" {
				fallbackRoot = nodeID
			}
		}
	}
	if rootID == "" {
		rootID = fallbackRoot
	}
	if rootID == "" {
		return "", false
	}

	// BFS following first child — with cycle prevention.
	var messages []messagePair
	currentID := rootID
	visited := make(map[string]bool)
	for currentID != "" && !visited[currentID] {
		visited[currentID] = true
		nodeRaw, ok := mapping[currentID]
		if !ok {
			break
		}
		node, ok := nodeRaw.(map[string]any)
		if !ok {
			break
		}
		if msg, ok := node["message"].(map[string]any); ok {
			author, _ := msg["author"].(map[string]any)
			role, _ := author["role"].(string)
			contentMap, _ := msg["content"].(map[string]any)
			var text string
			if contentMap != nil {
				parts, _ := contentMap["parts"].([]any)
				var textParts []string
				for _, p := range parts {
					if s, ok := p.(string); ok && s != "" {
						textParts = append(textParts, s)
					}
				}
				text = strings.TrimSpace(strings.Join(textParts, " "))
			}
			if role == "user" && text != "" {
				messages = append(messages, messagePair{Role: "user", Text: text})
			} else if role == "assistant" && text != "" {
				messages = append(messages, messagePair{Role: "assistant", Text: text})
			}
		}
		children, _ := node["children"].([]any)
		if len(children) > 0 {
			if childID, ok := children[0].(string); ok {
				currentID = childID
			} else {
				currentID = ""
			}
		} else {
			currentID = ""
		}
	}

	if len(messages) >= 2 {
		return messagesToTranscript(messages, true), true
	}
	return "", false
}

// trySlackJSON parses Slack channel export.
// Port of _try_slack_json in normalize.py:240-269.
func trySlackJSON(data any) (string, bool) {
	list, ok := data.([]any)
	if !ok {
		return "", false
	}
	var messages []messagePair
	seenUsers := make(map[string]string)
	lastRole := ""
	for _, item := range list {
		msg, ok := item.(map[string]any)
		if !ok {
			continue
		}
		msgType, _ := msg["type"].(string)
		if msgType != "message" {
			continue
		}
		userID, _ := msg["user"].(string)
		if userID == "" {
			userID, _ = msg["username"].(string)
		}
		text, _ := msg["text"].(string)
		text = strings.TrimSpace(text)
		if text == "" || userID == "" {
			continue
		}
		if _, seen := seenUsers[userID]; !seen {
			switch {
			case len(seenUsers) == 0:
				seenUsers[userID] = "user"
			case lastRole == "user":
				seenUsers[userID] = "assistant"
			default:
				seenUsers[userID] = "user"
			}
		}
		lastRole = seenUsers[userID]
		messages = append(messages, messagePair{Role: seenUsers[userID], Text: text})
	}
	if len(messages) >= 2 {
		return messagesToTranscript(messages, true), true
	}
	return "", false
}

// extractContent pulls text from content — handles string, []any (text blocks),
// or map with "text" key. Port of _extract_content in normalize.py:273-287.
func extractContent(content any) string {
	switch v := content.(type) {
	case string:
		return strings.TrimSpace(v)
	case []any:
		var parts []string
		for _, item := range v {
			switch it := item.(type) {
			case string:
				parts = append(parts, it)
			case map[string]any:
				if it["type"] == "text" {
					if text, ok := it["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.TrimSpace(strings.Join(parts, " "))
	case map[string]any:
		if text, ok := v["text"].(string); ok {
			return strings.TrimSpace(text)
		}
	}
	return ""
}

// messagesToTranscript converts a slice of messagePairs to transcript format
// with > markers. Port of _messages_to_transcript in normalize.py:290-319.
// When doSpellcheck is true, user turns are passed through spellcheck.
func messagesToTranscript(messages []messagePair, doSpellcheck bool) string {
	var lines []string
	i := 0
	for i < len(messages) {
		role := messages[i].Role
		text := messages[i].Text
		if role == "user" {
			if doSpellcheck {
				text = spellcheck.SpellcheckUserText(text, nil)
			}
			lines = append(lines, "> "+text)
			if i+1 < len(messages) && messages[i+1].Role == "assistant" {
				lines = append(lines, messages[i+1].Text)
				i += 2
			} else {
				i++
			}
		} else {
			lines = append(lines, text)
			i++
		}
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
