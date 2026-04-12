// Package normalize converts raw source files into plain text that the
// miner can chunk and embed. Phase B implements only the plain-text
// pass-through path: stat → 500 MB cap → read as UTF-8. Chat-export JSON
// normalization (claude.ai, ChatGPT, Slack, codex, claude-code) is Phase C.
package normalize

import (
	"fmt"
	"os"
)

// MaxFileSize is the safety cap borrowed from mempalace/normalize.py:32 —
// any file larger than 500 MB is rejected outright to protect the miner
// from accidental huge inputs.
const MaxFileSize int64 = 500 * 1024 * 1024

// Normalize loads path and returns its contents as a string. Invalid UTF-8
// is preserved byte-for-byte, matching Python's errors="replace" closely
// enough for Phase B's ASCII-only fixture set. Files over MaxFileSize are
// rejected with a wrapped error; stat/read failures are wrapped too.
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
	return string(data), nil
}
