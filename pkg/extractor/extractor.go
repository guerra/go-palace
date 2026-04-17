package extractor

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Extract classifies content per-segment and returns one Classification per
// segment that meets the default min-confidence threshold (0.3). Segments
// below threshold or with no marker hits are filtered out. Returns an
// empty slice (not nil) on empty input.
func Extract(content string) []Classification {
	return ExtractWith(content, ExtractorOptions{})
}

// ExtractWith is Extract with caller-supplied options. MinConfidence == 0
// uses the default 0.3; > 0 is the exact threshold; < 0 disables the filter.
func ExtractWith(content string, opts ExtractorOptions) []Classification {
	segs := ExtractSegments(content, opts)
	results := make([]Classification, 0, len(segs))
	for _, s := range segs {
		if s.Classification.Type != "" {
			results = append(results, s.Classification)
		}
	}
	return results
}

// ExtractSegments returns every segment from content paired with its
// classification. Segments that fall below the threshold or the length
// floor are kept in the result with Classification.Type == "" so callers
// can access the original text (used by internal/convominer).
// Classification.Index is always set to the 0-based segment position,
// regardless of whether the segment classified — callers can use Index
// to recover the original segment order even for dropped segments.
func ExtractSegments(content string, opts ExtractorOptions) []Segment {
	segments := splitIntoSegments(content)
	out := make([]Segment, 0, len(segments))
	threshold := opts.threshold()

	for i, para := range segments {
		seg := Segment{
			Content:        strings.TrimSpace(para),
			Classification: Classification{Index: i},
		}
		if len(seg.Content) < segmentMinLen {
			out = append(out, seg)
			continue
		}

		prose := extractProse(para)
		scores := make(map[ClassificationType]float64, len(AllTypes))
		bestEvidence := make(map[ClassificationType]string, len(AllTypes))

		for _, t := range AllTypes {
			s, best := scoreMarkers(prose, allMarkers[t])
			if s > 0 {
				scores[t] = s
				bestEvidence[t] = best
			}
		}

		if len(scores) == 0 {
			out = append(out, seg)
			continue
		}

		lengthBonus := 0.0
		switch {
		case len(para) > 500:
			lengthBonus = 2
		case len(para) > 200:
			lengthBonus = 1
		}

		// Stable max via AllTypes order (tie-break: earlier type wins).
		var maxType ClassificationType
		maxScore := 0.0
		for _, t := range AllTypes {
			if s := scores[t]; s > maxScore {
				maxScore = s
				maxType = t
			}
		}

		maxScore += lengthBonus
		origType := maxType
		maxType = disambiguate(maxType, prose, scores)

		confidence := maxScore / 5.0
		if confidence > 1.0 {
			confidence = 1.0
		}
		if confidence < threshold {
			out = append(out, seg)
			continue
		}

		// Evidence follows the winning type. If disambiguate switched types,
		// prefer the evidence for the NEW type; fall back to the original
		// type's evidence if no new-type marker hit (e.g. resolution without
		// milestone keywords).
		evidence := bestEvidence[maxType]
		if evidence == "" {
			evidence = bestEvidence[origType]
		}

		seg.Classification = Classification{
			Type:       maxType,
			Evidence:   evidence,
			Confidence: confidence,
			Index:      i,
		}
		out = append(out, seg)
	}

	return out
}

// Classify scores a SINGLE segment (no segment splitting) and returns the
// winning Classification. The caller is responsible for prose extraction
// (Classify does not strip code blocks). Returns (Classification{}, false)
// when no markers hit.
func Classify(segment string, opts ExtractorOptions) (Classification, bool) {
	threshold := opts.threshold()
	trimmed := strings.TrimSpace(segment)
	if len(trimmed) == 0 {
		return Classification{}, false
	}

	scores := make(map[ClassificationType]float64, len(AllTypes))
	bestEvidence := make(map[ClassificationType]string, len(AllTypes))
	for _, t := range AllTypes {
		s, best := scoreMarkers(trimmed, allMarkers[t])
		if s > 0 {
			scores[t] = s
			bestEvidence[t] = best
		}
	}
	if len(scores) == 0 {
		return Classification{}, false
	}

	lengthBonus := 0.0
	switch {
	case len(segment) > 500:
		lengthBonus = 2
	case len(segment) > 200:
		lengthBonus = 1
	}

	var maxType ClassificationType
	maxScore := 0.0
	for _, t := range AllTypes {
		if s := scores[t]; s > maxScore {
			maxScore = s
			maxType = t
		}
	}
	maxScore += lengthBonus
	origType := maxType
	maxType = disambiguate(maxType, trimmed, scores)

	confidence := maxScore / 5.0
	if confidence > 1.0 {
		confidence = 1.0
	}
	if confidence < threshold {
		return Classification{}, false
	}

	evidence := bestEvidence[maxType]
	if evidence == "" {
		evidence = bestEvidence[origType]
	}
	return Classification{
		Type:       maxType,
		Evidence:   evidence,
		Confidence: confidence,
		Index:      0,
	}, true
}

// scoreMarkers counts the number of marker-hits in text and returns both the
// total count AND the longest matched substring across all patterns. The
// longest string becomes the Classification.Evidence value. Scoring is
// case-insensitive via the marker patterns' own `(?i)` flags.
func scoreMarkers(text string, compiled []*regexp.Regexp) (float64, string) {
	lower := strings.ToLower(text)
	score := 0.0
	longest := ""
	for _, re := range compiled {
		matches := re.FindAllString(lower, -1)
		score += float64(len(matches))
		for _, m := range matches {
			if len(m) > len(longest) {
				longest = m
			}
		}
	}
	return score, longest
}

// getSentiment returns "positive", "negative", or "neutral" based on which
// sentiment word set dominates the unique lowercase tokens of text.
func getSentiment(text string) string {
	words := wordRe.FindAllString(text, -1)
	seen := make(map[string]bool, len(words))
	for _, w := range words {
		seen[strings.ToLower(w)] = true
	}
	pos, neg := 0, 0
	for w := range seen {
		if positiveWords[w] {
			pos++
		}
		if negativeWords[w] {
			neg++
		}
	}
	if pos > neg {
		return "positive"
	}
	if neg > pos {
		return "negative"
	}
	return "neutral"
}

// hasResolution returns true if any resolution pattern matches text.
func hasResolution(text string) bool {
	lower := strings.ToLower(text)
	for _, re := range resolutionPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

// disambiguate applies the Python oracle's disambiguation rules:
//   - problem + resolution → milestone (or emotion if scores["emotion"] > 0
//     AND sentiment is positive)
//   - problem + positive sentiment → milestone or emotion if their scores
//     are non-zero.
func disambiguate(memType ClassificationType, text string, scores map[ClassificationType]float64) ClassificationType {
	sentiment := getSentiment(text)

	if memType == TypeProblem && hasResolution(text) {
		if scores[TypeEmotion] > 0 && sentiment == "positive" {
			return TypeEmotion
		}
		return TypeMilestone
	}

	if memType == TypeProblem && sentiment == "positive" {
		if scores[TypeMilestone] > 0 {
			return TypeMilestone
		}
		if scores[TypeEmotion] > 0 {
			return TypeEmotion
		}
	}

	return memType
}

// isCodeLine returns true if the trimmed line looks like code rather than
// prose. Matches one of the compiled codeLinePatterns OR has a low alpha
// ratio (< 0.4 among > 10 runes).
func isCodeLine(line string) bool {
	stripped := strings.TrimSpace(line)
	if stripped == "" {
		return false
	}
	for _, re := range codeLinePatterns {
		if re.MatchString(stripped) {
			return true
		}
	}
	alphaCount := 0
	for _, c := range stripped {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			alphaCount++
		}
	}
	runes := utf8.RuneCountInString(stripped)
	if runes < 1 {
		runes = 1
	}
	ratio := float64(alphaCount) / float64(runes)
	if ratio < 0.4 && len(stripped) > 10 {
		return true
	}
	return false
}

// extractProse returns text with triple-backtick code fences and code-like
// lines stripped out. If the stripped result is empty, the original text is
// returned unchanged (fallback: better to classify over raw than ignore a
// segment entirely).
func extractProse(text string) string {
	lines := strings.Split(text, "\n")
	var prose []string
	inCode := false
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inCode = !inCode
			continue
		}
		if inCode {
			continue
		}
		if !isCodeLine(line) {
			prose = append(prose, line)
		}
	}
	result := strings.TrimSpace(strings.Join(prose, "\n"))
	if result == "" {
		return text
	}
	return result
}

// splitIntoSegments splits content into classifiable segments. The strategy
// is layered:
//  1. if >= 3 turn-marker lines exist, split by turns;
//  2. else split by blank lines (paragraphs);
//  3. if that produces <= 1 paragraph and the text spans > 20 lines, chop
//     into 25-line blocks.
func splitIntoSegments(text string) []string {
	lines := strings.Split(text, "\n")

	turnCount := 0
	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		for _, pat := range turnPatterns {
			if pat.MatchString(stripped) {
				turnCount++
				break
			}
		}
	}

	if turnCount >= 3 {
		return splitByTurns(lines)
	}

	paragraphs := make([]string, 0)
	for _, p := range strings.Split(text, "\n\n") {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			paragraphs = append(paragraphs, trimmed)
		}
	}

	if len(paragraphs) <= 1 && len(lines) > 20 {
		segments := make([]string, 0)
		for i := 0; i < len(lines); i += 25 {
			end := i + 25
			if end > len(lines) {
				end = len(lines)
			}
			group := strings.TrimSpace(strings.Join(lines[i:end], "\n"))
			if group != "" {
				segments = append(segments, group)
			}
		}
		return segments
	}

	return paragraphs
}

func splitByTurns(lines []string) []string {
	var segments []string
	var current []string

	for _, line := range lines {
		stripped := strings.TrimSpace(line)
		isTurn := false
		for _, pat := range turnPatterns {
			if pat.MatchString(stripped) {
				isTurn = true
				break
			}
		}

		if isTurn && len(current) > 0 {
			segments = append(segments, strings.Join(current, "\n"))
			current = []string{line}
		} else {
			current = append(current, line)
		}
	}

	if len(current) > 0 {
		segments = append(segments, strings.Join(current, "\n"))
	}

	return segments
}
