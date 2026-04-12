package miner

import "strings"

// Chunk-size constants. Byte-level to match Python's string slicing on
// ASCII-only fixtures. Non-ASCII content is a known Phase D rune-aware
// rewrite target.
const (
	// ChunkSize is the upper bound on a single drawer's document length.
	ChunkSize = 800
	// ChunkOverlap is the number of trailing bytes re-included in the next
	// chunk to avoid splitting ideas across a hard boundary.
	ChunkOverlap = 100
	// MinChunkSize filters out tiny fragments (e.g. "just the trailing
	// whitespace after a good split") that would pollute the palace.
	MinChunkSize = 50
)

// Chunk is one drawer-sized slice of text cut from a source file.
type Chunk struct {
	Content string
	Index   int
}

// ChunkText slices content into overlap-linked chunks. Direct port of
// chunk_text in mempalace/miner.py:325-365. Operates on bytes; Python's
// len(str) is codepoints but the behavior is identical on ASCII input.
//
// TODO(phase-d): switch to rune-based slicing for non-ASCII content so
// the Python oracle and Go path agree on multibyte-character fixtures.
func ChunkText(content string) []Chunk {
	content = strings.TrimSpace(content)
	if content == "" {
		return nil
	}
	var chunks []Chunk
	start, idx := 0, 0
	for start < len(content) {
		end := start + ChunkSize
		if end > len(content) {
			end = len(content)
		}
		if end < len(content) {
			window := content[start:end]
			// pos is window-relative: Go's LastIndex on the slice
			// matches Python's rfind(start,end) - start. The ChunkSize/2
			// guard prevents splitting too early (same as Python's
			// newline_pos > start + CHUNK_SIZE // 2).
			if pos := strings.LastIndex(window, "\n\n"); pos > ChunkSize/2 {
				end = start + pos
			} else if pos := strings.LastIndex(window, "\n"); pos > ChunkSize/2 {
				end = start + pos
			}
		}
		chunk := strings.TrimSpace(content[start:end])
		if len(chunk) >= MinChunkSize {
			chunks = append(chunks, Chunk{Content: chunk, Index: idx})
			idx++
		}
		if end < len(content) {
			start = end - ChunkOverlap
		} else {
			start = end
		}
	}
	return chunks
}
