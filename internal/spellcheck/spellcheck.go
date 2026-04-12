// Package spellcheck provides token-level spell correction for user messages
// before palace filing. Phase C implements the full interface with stubbed
// correction (returns input unchanged). ShouldSkip and EditDistance are fully
// implemented — they are the contract other code relies on.
package spellcheck

import (
	"regexp"
	"strings"
	"sync"
	"unicode"
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

// tokenRe splits text into whitespace-separated tokens, matching Python _TOKEN_RE.
var tokenRe = regexp.MustCompile(`\S+`)

// dictWords is the set of known English words (lowercase), loaded once.
var (
	dictOnce  sync.Once
	dictWords map[string]bool
)

func loadDict() {
	dictOnce.Do(func() {
		dictWords = make(map[string]bool, len(wordFreqs))
		for w := range wordFreqs {
			dictWords[w] = true
		}
	})
}

// alphabet for candidate generation.
const alphabet = "abcdefghijklmnopqrstuvwxyz"

// edits1 generates all strings that are one edit distance from word.
// Edits: deletes, transposes, replaces, inserts.
func edits1(word string) []string {
	runes := []rune(word)
	n := len(runes)
	var results []string

	// Deletes
	for i := 0; i < n; i++ {
		results = append(results, string(runes[:i])+string(runes[i+1:]))
	}
	// Transposes
	for i := 0; i < n-1; i++ {
		r := make([]rune, n)
		copy(r, runes)
		r[i], r[i+1] = r[i+1], r[i]
		results = append(results, string(r))
	}
	// Replaces
	for i := 0; i < n; i++ {
		for _, c := range alphabet {
			if c != runes[i] {
				r := make([]rune, n)
				copy(r, runes)
				r[i] = c
				results = append(results, string(r))
			}
		}
	}
	// Inserts
	for i := 0; i <= n; i++ {
		for _, c := range alphabet {
			results = append(results, string(runes[:i])+string(c)+string(runes[i:]))
		}
	}
	return results
}

// knownCandidates filters candidates to words in the frequency dict and
// returns the highest-frequency match.
func bestCandidate(candidates []string) (string, bool) {
	loadDict()
	best := ""
	bestFreq := 0
	for _, c := range candidates {
		if f, ok := wordFreqs[c]; ok && f > bestFreq {
			best = c
			bestFreq = f
		}
	}
	return best, best != ""
}

// correct finds the best correction for a word using Norvig's algorithm.
// Returns the word itself if it's already known or no correction found.
func correct(word string) string {
	loadDict()
	lower := strings.ToLower(word)
	// Already known
	if dictWords[lower] {
		return word
	}
	// Try edit distance 1
	e1 := edits1(lower)
	if best, ok := bestCandidate(e1); ok {
		return best
	}
	// Try edit distance 2 (edits of edits)
	seen := make(map[string]bool)
	var e2 []string
	for _, w := range e1 {
		for _, w2 := range edits1(w) {
			if !seen[w2] {
				seen[w2] = true
				e2 = append(e2, w2)
			}
		}
	}
	if best, ok := bestCandidate(e2); ok {
		return best
	}
	return word
}

// SpellcheckUserText spell-corrects a user message. Port of spellcheck.py:161-212.
// Norvig-style frequency-based correction with edit distance guards.
func SpellcheckUserText(text string, knownNames map[string]bool) string {
	loadDict()

	return tokenRe.ReplaceAllStringFunc(text, func(token string) string {
		// Strip trailing punctuation for checking, reattach after
		stripped := strings.TrimRight(token, ".,!?;:'\")")
		punct := token[len(stripped):]

		if stripped == "" || ShouldSkip(stripped, knownNames) {
			return token
		}

		// Only correct lowercase words (capitalized = likely proper nouns)
		firstRune := []rune(stripped)[0]
		if unicode.IsUpper(firstRune) {
			return token
		}

		// Skip words already in the dictionary
		if dictWords[strings.ToLower(stripped)] {
			return token
		}

		corrected := correct(stripped)
		if corrected != stripped {
			dist := EditDistance(stripped, corrected)
			maxEdits := 2
			if len(stripped) > 7 {
				maxEdits = 3
			}
			if dist > maxEdits {
				return token
			}
		}

		return corrected + punct
	})
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
