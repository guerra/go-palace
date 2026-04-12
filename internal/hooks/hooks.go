// Package hooks implements harness hook handlers (session-start, stop,
// precompact). Reads JSON from stdin, outputs JSON to stdout.
// Ports hooks_cli.py.
package hooks

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// SaveInterval is the number of human messages between auto-save triggers.
const SaveInterval = 15

// StopBlockReason is the save instruction emitted when the stop hook triggers.
const StopBlockReason = "AUTO-SAVE checkpoint. Save key topics, decisions, quotes, and code " +
	"from this session to your memory system. Organize into appropriate " +
	"categories. Use verbatim quotes where possible. Continue conversation " +
	"after saving."

// PrecompactBlockReason is the save instruction emitted before context compaction.
const PrecompactBlockReason = "COMPACTION IMMINENT. Save ALL topics, decisions, quotes, code, and " +
	"important context from this session to your memory system. Be thorough " +
	"\u2014 after compaction, detailed context will be lost. Organize into " +
	"appropriate categories. Use verbatim quotes where possible. Save " +
	"everything, then allow compaction to proceed."

// HookInput is the JSON structure read from stdin.
type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	StopHookActive json.RawMessage `json:"stop_hook_active"`
}

// HookOutput is the JSON structure written to stdout.
type HookOutput struct {
	Decision string `json:"decision,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

// stateDir can be overridden for testing via MEMPALACE_HOOK_STATE_DIR.
func stateDir() string {
	if d := os.Getenv("MEMPALACE_HOOK_STATE_DIR"); d != "" {
		return d
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".mempalace", "hook_state")
	}
	return filepath.Join(home, ".mempalace", "hook_state")
}

var supportedHarnesses = map[string]bool{"claude-code": true, "codex": true}

// RunHook reads JSON from stdin, dispatches to the named hook handler, and
// writes JSON to stdout.
func RunHook(hookName, harness string, stdin io.Reader, stdout io.Writer) error {
	if !supportedHarnesses[harness] {
		return fmt.Errorf("hooks: unknown harness: %s", harness)
	}

	var input HookInput
	if err := json.NewDecoder(stdin).Decode(&input); err != nil {
		// Proceed with empty data like Python does.
		input = HookInput{}
	}
	input.SessionID = sanitizeSessionID(input.SessionID)

	switch hookName {
	case "session-start":
		return hookSessionStart(input, stdout)
	case "stop":
		return hookStop(input, stdout)
	case "precompact":
		return hookPrecompact(input, stdout)
	default:
		return fmt.Errorf("hooks: unknown hook: %s", hookName)
	}
}

func hookSessionStart(input HookInput, stdout io.Writer) error {
	logEntry(fmt.Sprintf("SESSION START for session %s", input.SessionID))
	_ = os.MkdirAll(stateDir(), 0o755)
	writeOutput(stdout, HookOutput{})
	return nil
}

func hookPrecompact(input HookInput, stdout io.Writer) error {
	logEntry(fmt.Sprintf("PRE-COMPACT triggered for session %s", input.SessionID))
	writeOutput(stdout, HookOutput{Decision: "block", Reason: PrecompactBlockReason})
	return nil
}

func hookStop(input HookInput, stdout io.Writer) error {
	// If already in a save cycle, pass through.
	if isStopHookActive(input.StopHookActive) {
		writeOutput(stdout, HookOutput{})
		return nil
	}

	exchangeCount := countHumanMessages(input.TranscriptPath)

	dir := stateDir()
	_ = os.MkdirAll(dir, 0o755)
	lastSaveFile := filepath.Join(dir, input.SessionID+"_last_save")

	lastSave := 0
	if data, err := os.ReadFile(lastSaveFile); err == nil {
		if n, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
			lastSave = n
		}
	}

	sinceLast := exchangeCount - lastSave
	logEntry(fmt.Sprintf("Session %s: %d exchanges, %d since last save",
		input.SessionID, exchangeCount, sinceLast))

	if sinceLast >= SaveInterval && exchangeCount > 0 {
		_ = os.WriteFile(lastSaveFile, []byte(strconv.Itoa(exchangeCount)), 0o644)
		logEntry(fmt.Sprintf("TRIGGERING SAVE at exchange %d", exchangeCount))
		writeOutput(stdout, HookOutput{Decision: "block", Reason: StopBlockReason})
	} else {
		writeOutput(stdout, HookOutput{})
	}
	return nil
}

func isStopHookActive(raw json.RawMessage) bool {
	if len(raw) == 0 {
		return false
	}
	// Try bool first.
	var b bool
	if json.Unmarshal(raw, &b) == nil {
		return b
	}
	// Try string: "true", "1", "yes".
	var s string
	if json.Unmarshal(raw, &s) == nil {
		s = strings.ToLower(s)
		return s == "true" || s == "1" || s == "yes"
	}
	return false
}

// countHumanMessages reads a JSONL transcript and counts human messages,
// excluding command-messages. Handles Claude Code and Codex formats.
func countHumanMessages(transcriptPath string) int {
	if transcriptPath == "" {
		return 0
	}
	f, err := os.Open(transcriptPath)
	if err != nil {
		return 0
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}

		// Claude Code format: {"message":{"role":"user","content":"..."}}
		if msg, ok := entry["message"].(map[string]any); ok {
			if role, _ := msg["role"].(string); role == "user" {
				content := msg["content"]
				if hasCommandMessage(content) {
					continue
				}
				count++
				continue
			}
		}

		// Codex format: {"type":"event_msg","payload":{"type":"user_message","message":"..."}}
		if t, _ := entry["type"].(string); t == "event_msg" {
			if payload, ok := entry["payload"].(map[string]any); ok {
				if pt, _ := payload["type"].(string); pt == "user_message" {
					if msgText, _ := payload["message"].(string); !strings.Contains(msgText, "<command-message>") {
						count++
					}
				}
			}
		}
	}
	// Silently accept partial count on scanner error (I/O failure or
	// oversized line). Transcript files are local and best-effort.
	_ = scanner.Err()
	return count
}

func hasCommandMessage(content any) bool {
	switch c := content.(type) {
	case string:
		return strings.Contains(c, "<command-message>")
	case []any:
		for _, item := range c {
			if m, ok := item.(map[string]any); ok {
				if text, _ := m["text"].(string); strings.Contains(text, "<command-message>") {
					return true
				}
			}
		}
	}
	return false
}

var reSanitizeID = regexp.MustCompile(`[^a-zA-Z0-9_-]`)

func sanitizeSessionID(id string) string {
	s := reSanitizeID.ReplaceAllString(id, "")
	if s == "" {
		return "unknown"
	}
	return s
}

func writeOutput(w io.Writer, out HookOutput) {
	_ = json.NewEncoder(w).Encode(out)
}

func logEntry(message string) {
	dir := stateDir()
	_ = os.MkdirAll(dir, 0o755)
	logPath := filepath.Join(dir, "hook.log")
	ts := time.Now().Format("15:04:05")
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "[%s] %s\n", ts, message)
}
