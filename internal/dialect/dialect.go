// Package dialect implements the AAAK structured symbolic summary format.
// Port of Python dialect.py compress/decode for plain text (not zettel encoding).
package dialect

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const ProtocolVersion = "AAAK-1.0"

// wordRe extracts alphanumeric words of 3+ chars for topic extraction.
var wordRe = regexp.MustCompile(`[a-zA-Z][a-zA-Z_-]{2,}`)

// sentenceRe splits text into sentences.
var sentenceRe = regexp.MustCompile(`[.!?\n]+`)

// nonAlpha strips non-alpha chars for entity detection.
var nonAlpha = regexp.MustCompile(`[^a-zA-Z]`)

// DecodeResult holds the parsed output of an AAAK dialect string.
type DecodeResult struct {
	// Header holds metadata from the header line (field semantics vary by format:
	// zettel format uses file/entities/date/title; plain-text compress uses wing/room/date/stem).
	Header  map[string]string
	Arc     string
	Zettels []string
	Tunnels []string
}

// Stats holds compression statistics for a text-to-AAAK conversion.
type Stats struct {
	OriginalTokensEst int
	SummaryTokensEst  int
	SizeRatio         string
	OriginalChars     int
	SummaryChars      int
}

// Dialect holds entity codes and skip lists for AAAK encoding.
type Dialect struct {
	EntityCodes map[string]string
	SkipNames   []string
}

// New creates a Dialect with the given entity codes and skip names.
func New(entities map[string]string, skipNames []string) *Dialect {
	d := &Dialect{
		EntityCodes: make(map[string]string),
		SkipNames:   make([]string, 0),
	}
	for name, code := range entities {
		d.EntityCodes[name] = code
		d.EntityCodes[strings.ToLower(name)] = code
	}
	for _, n := range skipNames {
		d.SkipNames = append(d.SkipNames, strings.ToLower(n))
	}
	return d
}

// EncodeEntity converts a name to its short code.
// Port of Python Dialect.encode_entity (dialect.py:373-385).
func (d *Dialect) EncodeEntity(name string) string {
	lower := strings.ToLower(name)
	for _, skip := range d.SkipNames {
		if strings.Contains(lower, skip) {
			return ""
		}
	}
	if code, ok := d.EntityCodes[name]; ok {
		return code
	}
	if code, ok := d.EntityCodes[lower]; ok {
		return code
	}
	for key, code := range d.EntityCodes {
		if strings.Contains(lower, strings.ToLower(key)) {
			return code
		}
	}
	// Auto-code: first 3 chars uppercase
	if len(name) >= 3 {
		return strings.ToUpper(name[:3])
	}
	return strings.ToUpper(name)
}

// EncodeEmotions converts an emotion list to compact codes.
func (d *Dialect) EncodeEmotions(emotions []string) string {
	var codes []string
	seen := make(map[string]bool)
	for _, e := range emotions {
		code, ok := EmotionCodes[e]
		if !ok {
			if len(e) >= 4 {
				code = e[:4]
			} else {
				code = e
			}
		}
		if !seen[code] {
			seen[code] = true
			codes = append(codes, code)
		}
		if len(codes) >= 3 {
			break
		}
	}
	return strings.Join(codes, "+")
}

// detectEmotions finds emotion signals in text. Port of dialect.py:414-423.
// Keys are sorted for deterministic output across runs.
func (d *Dialect) detectEmotions(text string) []string {
	lower := strings.ToLower(text)
	var detected []string
	seen := make(map[string]bool)
	keys := make([]string, 0, len(EmotionSignals))
	for k := range EmotionSignals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, keyword := range keys {
		code := EmotionSignals[keyword]
		if strings.Contains(lower, keyword) && !seen[code] {
			detected = append(detected, code)
			seen[code] = true
		}
		if len(detected) >= 3 {
			break
		}
	}
	return detected
}

// detectFlags finds flag signals in text. Port of dialect.py:425-434.
// Keys are sorted for deterministic output across runs.
func (d *Dialect) detectFlags(text string) []string {
	lower := strings.ToLower(text)
	var detected []string
	seen := make(map[string]bool)
	keys := make([]string, 0, len(FlagSignals))
	for k := range FlagSignals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, keyword := range keys {
		flag := FlagSignals[keyword]
		if strings.Contains(lower, keyword) && !seen[flag] {
			detected = append(detected, flag)
			seen[flag] = true
		}
		if len(detected) >= 3 {
			break
		}
	}
	return detected
}

// extractTopics extracts key topic words from text. Port of dialect.py:436-461.
func (d *Dialect) extractTopics(text string, maxTopics int) []string {
	words := wordRe.FindAllString(text, -1)
	freq := make(map[string]int)
	for _, w := range words {
		lower := strings.ToLower(w)
		if stopWords[lower] || len(lower) < 3 {
			continue
		}
		freq[lower]++
	}
	// Boost proper nouns and technical terms
	for _, w := range words {
		lower := strings.ToLower(w)
		if stopWords[lower] {
			continue
		}
		if len(w) > 0 && w[0] >= 'A' && w[0] <= 'Z' {
			if _, ok := freq[lower]; ok {
				freq[lower] += 2
			}
		}
		if strings.Contains(w, "_") || strings.Contains(w, "-") {
			if _, ok := freq[lower]; ok {
				freq[lower] += 2
			}
		}
		// CamelCase boost
		for _, c := range w[1:] {
			if c >= 'A' && c <= 'Z' {
				if _, ok := freq[lower]; ok {
					freq[lower] += 2
				}
				break
			}
		}
	}

	type kv struct {
		word string
		cnt  int
	}
	ranked := make([]kv, 0, len(freq))
	for w, c := range freq {
		ranked = append(ranked, kv{w, c})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].cnt != ranked[j].cnt {
			return ranked[i].cnt > ranked[j].cnt
		}
		return ranked[i].word < ranked[j].word
	})

	result := make([]string, 0, maxTopics)
	for _, kv := range ranked {
		result = append(result, kv.word)
		if len(result) >= maxTopics {
			break
		}
	}
	return result
}

// extractKeySentence finds the most important sentence. Port of dialect.py:463-514.
func (d *Dialect) extractKeySentence(text string) string {
	parts := sentenceRe.Split(text, -1)
	var sentences []string
	for _, s := range parts {
		s = strings.TrimSpace(s)
		if len(s) > 10 {
			sentences = append(sentences, s)
		}
	}
	if len(sentences) == 0 {
		return ""
	}

	decisionWords := map[string]bool{
		"decided": true, "because": true, "instead": true, "prefer": true,
		"switched": true, "chose": true, "realized": true, "important": true,
		"key": true, "critical": true, "discovered": true, "learned": true,
		"conclusion": true, "solution": true, "reason": true, "why": true,
		"breakthrough": true, "insight": true,
	}

	type scored struct {
		score int
		sent  string
	}
	var results []scored
	for _, s := range sentences {
		score := 0
		lower := strings.ToLower(s)
		for w := range decisionWords {
			if strings.Contains(lower, w) {
				score += 2
			}
		}
		if len(s) < 80 {
			score++
		}
		if len(s) < 40 {
			score++
		}
		if len(s) > 150 {
			score -= 2
		}
		results = append(results, scored{score, s})
	}

	sort.SliceStable(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	best := results[0].sent
	if len(best) > 55 {
		best = best[:52] + "..."
	}
	return best
}

// detectEntitiesInText finds known entities or capitalized words.
// Port of dialect.py:516-543.
func (d *Dialect) detectEntitiesInText(text string) []string {
	var found []string
	lower := strings.ToLower(text)

	// Check known entities first
	for name, code := range d.EntityCodes {
		if name == strings.ToLower(name) {
			continue // skip lowercase duplicates
		}
		if strings.Contains(lower, strings.ToLower(name)) {
			dup := false
			for _, f := range found {
				if f == code {
					dup = true
					break
				}
			}
			if !dup {
				found = append(found, code)
			}
		}
	}
	if len(found) > 0 {
		return found
	}

	// Fallback: find capitalized words not at sentence start
	words := strings.Fields(text)
	for i, w := range words {
		clean := nonAlpha.ReplaceAllString(w, "")
		if len(clean) < 2 {
			continue
		}
		first := rune(clean[0])
		if first < 'A' || first > 'Z' {
			continue
		}
		rest := clean[1:]
		if strings.ToLower(rest) != rest {
			continue
		}
		if i == 0 {
			continue
		}
		if stopWords[strings.ToLower(clean)] {
			continue
		}
		code := strings.ToUpper(clean[:3])
		dup := false
		for _, f := range found {
			if f == code {
				dup = true
				break
			}
		}
		if !dup {
			found = append(found, code)
		}
		if len(found) >= 3 {
			break
		}
	}
	return found
}

// Compress summarizes plain text into AAAK Dialect format.
// Port of Python Dialect.compress (dialect.py:545-608).
// metadata is variadic to preserve backward compatibility with existing callers.
func (d *Dialect) Compress(text string, metadata ...map[string]string) string {
	meta := map[string]string{}
	if len(metadata) > 0 && metadata[0] != nil {
		meta = metadata[0]
	}

	entities := d.detectEntitiesInText(text)
	entityStr := "???"
	if len(entities) > 0 {
		if len(entities) > 3 {
			entities = entities[:3]
		}
		entityStr = strings.Join(entities, "+")
	}

	topics := d.extractTopics(text, 3)
	topicStr := "misc"
	if len(topics) > 0 {
		topicStr = strings.Join(topics, "_")
	}

	quote := d.extractKeySentence(text)
	quotePart := ""
	if quote != "" {
		quotePart = fmt.Sprintf(`"%s"`, quote)
	}

	emotions := d.detectEmotions(text)
	emotionStr := strings.Join(emotions, "+")

	flags := d.detectFlags(text)
	flagStr := strings.Join(flags, "+")

	source := meta["source_file"]
	wing := meta["wing"]
	room := meta["room"]
	date := meta["date"]

	var lines []string

	// Header line if metadata available
	if source != "" || wing != "" {
		headerParts := []string{
			orDefault(wing, "?"),
			orDefault(room, "?"),
			orDefault(date, "?"),
			orDefault(stemName(source), "?"),
		}
		lines = append(lines, strings.Join(headerParts, "|"))
	}

	// Content line: 0:ENTITIES|topics|"quote"|emotions|flags
	parts := []string{fmt.Sprintf("0:%s", entityStr), topicStr}
	if quotePart != "" {
		parts = append(parts, quotePart)
	}
	if emotionStr != "" {
		parts = append(parts, emotionStr)
	}
	if flagStr != "" {
		parts = append(parts, flagStr)
	}
	lines = append(lines, strings.Join(parts, "|"))

	return strings.Join(lines, "\n")
}

// Decode parses AAAK dialect text back into structured data.
// Port of Python Dialect.decode (dialect.py:912-933).
//
// Header field semantics depend on the source format:
//   - Zettel format: file | entities | date | title
//   - Plain-text compress: wing | room | date | stem
//
// The parser uses generic keys (field_0..field_3) since both formats share the
// same pipe-delimited structure.
func (d *Dialect) Decode(dialectText string) DecodeResult {
	lines := strings.Split(strings.TrimSpace(dialectText), "\n")
	result := DecodeResult{
		Header: map[string]string{},
	}

	for _, line := range lines {
		switch {
		case strings.HasPrefix(line, "ARC:"):
			result.Arc = line[4:]
		case strings.HasPrefix(line, "T:"):
			result.Tunnels = append(result.Tunnels, line)
		case strings.Contains(line, "|") && strings.Contains(strings.SplitN(line, "|", 2)[0], ":"):
			result.Zettels = append(result.Zettels, line)
		case strings.Contains(line, "|"):
			parts := strings.Split(line, "|")
			header := map[string]string{}
			if len(parts) > 0 {
				header["field_0"] = parts[0]
			}
			if len(parts) > 1 {
				header["field_1"] = parts[1]
			}
			if len(parts) > 2 {
				header["field_2"] = parts[2]
			}
			if len(parts) > 3 {
				header["field_3"] = parts[3]
			}
			result.Header = header
		}
	}

	return result
}

// CountTokens estimates token count using word-based heuristic (~1.3 tokens/word).
// Port of Python Dialect.count_tokens (dialect.py:938-949).
func CountTokens(text string) int {
	words := strings.Fields(text)
	tokens := int(float64(len(words)) * 1.3)
	if tokens < 1 {
		return 1
	}
	return tokens
}

// CompressionStats returns size comparison data for text->AAAK conversion.
// Port of Python Dialect.compression_stats (dialect.py:951-967).
func (d *Dialect) CompressionStats(original, compressed string) Stats {
	origTokens := CountTokens(original)
	compTokens := CountTokens(compressed)
	ratio := float64(origTokens) / float64(max(compTokens, 1))
	return Stats{
		OriginalTokensEst: origTokens,
		SummaryTokensEst:  compTokens,
		SizeRatio:         fmt.Sprintf("%.1f", ratio),
		OriginalChars:     len(original),
		SummaryChars:      len(compressed),
	}
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func stemName(path string) string {
	if path == "" {
		return ""
	}
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	if ext != "" {
		return base[:len(base)-len(ext)]
	}
	return base
}
