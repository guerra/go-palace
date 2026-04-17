// Package normalize performs pre-embed content cleaning. Normalize is wired
// transparently into palace.UpsertBatch so stored drawers keep the caller's
// raw document while the embedder sees a hygienic form. This reduces
// false-negatives in semantic search caused by incidental whitespace, CRLF,
// or Unicode form differences.
package normalize

import (
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Package-level compiled regexes. Never compile inside the hot path —
// UpsertBatch calls Normalize per-drawer and batches can be 1000+.
var (
	// paragraphBreakRe collapses 2+ newlines to exactly two. Run BEFORE
	// whitespace collapse so paragraph boundaries are preserved.
	paragraphBreakRe = regexp.MustCompile(`\n{2,}`)
	// whitespaceRunRe collapses tabs, multiple spaces, and single newlines to
	// a single space. The paragraph sentinel protects double-newlines.
	whitespaceRunRe = regexp.MustCompile(`[ \t\r\n]+`)
)

// paragraphSentinel is a string unlikely to appear in any document. Used
// internally to protect paragraph breaks during whitespace collapse.
const paragraphSentinel = "\x00PARABREAK\x00"

// Normalize returns a hygienic form of s safe to embed:
//  1. invalid UTF-8 runes replaced with the Unicode replacement character,
//  2. NFC-normalized,
//  3. paragraph boundaries preserved as exactly one blank line between
//     paragraphs,
//  4. runs of whitespace (tabs, spaces, newlines) within paragraphs
//     collapsed to a single space,
//  5. leading / trailing whitespace trimmed.
//
// The stored palace document is never normalized — that remains the raw
// caller input. Only the embedder sees the Normalize output.
func Normalize(s string) string {
	if s == "" {
		return ""
	}
	// (1) Replace invalid UTF-8 bytes with the replacement rune.
	s = strings.ToValidUTF8(s, "\uFFFD")
	// (2) NFC normalize BEFORE whitespace collapse so any width-space runes
	//     introduced by decomposition are collapsed below.
	s = norm.NFC.String(s)
	// (3) Protect paragraph boundaries.
	s = paragraphBreakRe.ReplaceAllString(s, paragraphSentinel)
	// (4) Collapse remaining whitespace runs to a single space.
	s = whitespaceRunRe.ReplaceAllString(s, " ")
	// (5) Restore paragraph boundaries.
	s = strings.ReplaceAll(s, paragraphSentinel, "\n\n")
	// (6) Trim.
	s = strings.TrimSpace(s)
	return s
}
