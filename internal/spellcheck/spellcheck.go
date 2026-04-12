// Package spellcheck provides token-level spell correction for user messages
// before palace filing. Phase C implements the full interface with stubbed
// correction (returns input unchanged). ShouldSkip and EditDistance are fully
// implemented — they are the contract other code relies on.
package spellcheck

import (
	"regexp"
	"strings"
)

// MinLength is the minimum token length to consider for spellchecking.
// Shorter tokens are skipped (common abbreviations, pronouns, etc.).
const MinLength = 4

// Compiled patterns for skip detection — ported from spellcheck.py:66-81.
var (
	hasDigit      = regexp.MustCompile(`\d`)
	isCamel       = regexp.MustCompile(`[A-Z][a-z]+[A-Z]`)
	isAllCaps     = regexp.MustCompile(`^[A-Z_@#$%^&*()+=\[\]{}|<>?.:/\\]+$`)
	isTechnical   = regexp.MustCompile(`[-_]`)
	isURL         = regexp.MustCompile(`(?i)https?://|www\.|/Users/|~/|\.[a-z]{2,4}$`)
	isCodeOrEmoji = regexp.MustCompile("[`*_#{}\\[\\]\\\\]")
)

// ShouldSkip returns true if the token should be left as-is during
// spellchecking. Port of _should_skip in spellcheck.py:88-107.
func ShouldSkip(token string, knownNames map[string]bool) bool {
	if len(token) < MinLength {
		return true
	}
	if hasDigit.MatchString(token) {
		return true
	}
	if isCamel.MatchString(token) {
		return true
	}
	if isAllCaps.MatchString(token) {
		return true
	}
	if isTechnical.MatchString(token) {
		return true
	}
	if isURL.MatchString(token) {
		return true
	}
	if isCodeOrEmoji.MatchString(token) {
		return true
	}
	if knownNames != nil && knownNames[strings.ToLower(token)] {
		return true
	}
	return false
}

// EditDistance computes the Levenshtein distance between two strings.
// Port of _edit_distance in spellcheck.py:137-150.
func EditDistance(a, b string) int {
	if a == b {
		return 0
	}
	ra := []rune(a)
	rb := []rune(b)
	if len(ra) == 0 {
		return len(rb)
	}
	if len(rb) == 0 {
		return len(ra)
	}
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i, ca := range ra {
		curr := make([]int, len(rb)+1)
		curr[0] = i + 1
		for j, cb := range rb {
			del := prev[j+1] + 1
			ins := curr[j] + 1
			sub := prev[j]
			if ca != cb {
				sub++
			}
			best := del
			if ins < best {
				best = ins
			}
			if sub < best {
				best = sub
			}
			curr[j+1] = best
		}
		prev = curr
	}
	return prev[len(rb)]
}

// SpellcheckUserText is the main spell-correction entry point.
// STUB: returns text unchanged. The interface matches spellcheck.py:161-212
// but actual correction logic is deferred to Phase F. When autocorrect is
// not available in Python, it also returns text unchanged (spellcheck.py:44-45).
func SpellcheckUserText(text string, knownNames map[string]bool) string {
	return text
}

// SpellcheckTranscriptLine spell-corrects a single transcript line.
// Only touches lines that start with ">" (user turns). Assistant turns
// are never modified. Port of spellcheck.py:215-232.
func SpellcheckTranscriptLine(line string) string {
	stripped := strings.TrimLeft(line, " \t")
	if !strings.HasPrefix(stripped, ">") {
		return line
	}
	// BUG(phase-f): when line is ">text" (no space after >), prefixLen=2
	// slices to "est", dropping the first message char. Python has the
	// identical bug (spellcheck.py:226-228). Fix in Phase F when real
	// correction is enabled — dormant while SpellcheckUserText is a stub.
	prefixLen := len(line) - len(stripped) + 2 // "> "
	if prefixLen > len(line) {
		return line
	}
	message := line[prefixLen:]
	if strings.TrimSpace(message) == "" {
		return line
	}
	corrected := SpellcheckUserText(message, nil)
	return line[:prefixLen] + corrected
}

// SpellcheckTranscript spell-corrects all user turns in a full transcript.
// Only lines starting with ">" are touched. Port of spellcheck.py:235-241.
func SpellcheckTranscript(content string) string {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = SpellcheckTranscriptLine(line)
	}
	return strings.Join(lines, "\n")
}
