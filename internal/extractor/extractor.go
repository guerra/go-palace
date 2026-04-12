// Package extractor provides 5-type heuristic memory extraction from text.
// Types: decision, preference, milestone, problem, emotional.
// Pure keyword/pattern heuristics — no LLM required.
// Port of general_extractor.py.
package extractor

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Memory is one extracted memory from text.
type Memory struct {
	Content    string
	MemoryType string
	ChunkIndex int
}

// Marker sets — one per memory type.
// Ported exactly from general_extractor.py:30-161.
// All patterns use (?i) for case-insensitive matching where Python uses re.IGNORECASE.
var (
	DecisionMarkers = []string{
		`(?i)\blet'?s (use|go with|try|pick|choose|switch to)\b`,
		`(?i)\bwe (should|decided|chose|went with|picked|settled on)\b`,
		`(?i)\bi'?m going (to|with)\b`,
		`(?i)\bbetter (to|than|approach|option|choice)\b`,
		`(?i)\binstead of\b`,
		`(?i)\brather than\b`,
		`(?i)\bthe reason (is|was|being)\b`,
		`(?i)\bbecause\b`,
		`(?i)\btrade-?off\b`,
		`(?i)\bpros and cons\b`,
		`(?i)\bover\b.*\bbecause\b`,
		`(?i)\barchitecture\b`,
		`(?i)\bapproach\b`,
		`(?i)\bstrategy\b`,
		`(?i)\bpattern\b`,
		`(?i)\bstack\b`,
		`(?i)\bframework\b`,
		`(?i)\binfrastructure\b`,
		`(?i)\bset (it |this )?to\b`,
		`(?i)\bconfigure\b`,
		`(?i)\bdefault\b`,
	}

	PreferenceMarkers = []string{
		`(?i)\bi prefer\b`,
		`(?i)\balways use\b`,
		`(?i)\bnever use\b`,
		`(?i)\bdon'?t (ever |like to )?(use|do|mock|stub|import)\b`,
		`(?i)\bi like (to|when|how)\b`,
		`(?i)\bi hate (when|how|it when)\b`,
		`(?i)\bplease (always|never|don'?t)\b`,
		`(?i)\bmy (rule|preference|style|convention) is\b`,
		`(?i)\bwe (always|never)\b`,
		`(?i)\bfunctional\b.*\bstyle\b`,
		`(?i)\bimperative\b`,
		`(?i)\bsnake_?case\b`,
		`(?i)\bcamel_?case\b`,
		`(?i)\btabs\b.*\bspaces\b`,
		`(?i)\bspaces\b.*\btabs\b`,
		`(?i)\buse\b.*\binstead of\b`,
	}

	MilestoneMarkers = []string{
		`(?i)\bit works\b`,
		`(?i)\bit worked\b`,
		`(?i)\bgot it working\b`,
		`(?i)\bfixed\b`,
		`(?i)\bsolved\b`,
		`(?i)\bbreakthrough\b`,
		`(?i)\bfigured (it )?out\b`,
		`(?i)\bnailed it\b`,
		`(?i)\bcracked (it|the)\b`,
		`(?i)\bfinally\b`,
		`(?i)\bfirst time\b`,
		`(?i)\bfirst ever\b`,
		`(?i)\bnever (done|been|had) before\b`,
		`(?i)\bdiscovered\b`,
		`(?i)\brealized\b`,
		`(?i)\bfound (out|that)\b`,
		`(?i)\bturns out\b`,
		`(?i)\bthe key (is|was|insight)\b`,
		`(?i)\bthe trick (is|was)\b`,
		`(?i)\bnow i (understand|see|get it)\b`,
		`(?i)\bbuilt\b`,
		`(?i)\bcreated\b`,
		`(?i)\bimplemented\b`,
		`(?i)\bshipped\b`,
		`(?i)\blaunched\b`,
		`(?i)\bdeployed\b`,
		`(?i)\breleased\b`,
		`(?i)\bprototype\b`,
		`(?i)\bproof of concept\b`,
		`(?i)\bdemo\b`,
		`(?i)\bversion \d`,
		`(?i)\bv\d+\.\d+`,
		`(?i)\d+x (compression|faster|slower|better|improvement|reduction)`,
		`(?i)\d+% (reduction|improvement|faster|better|smaller)`,
	}

	ProblemMarkers = []string{
		`(?i)\b(bug|error|crash|fail|broke|broken|issue|problem)\b`,
		`(?i)\bdoesn'?t work\b`,
		`(?i)\bnot working\b`,
		`(?i)\bwon'?t\b.*\bwork\b`,
		`(?i)\bkeeps? (failing|crashing|breaking|erroring)\b`,
		`(?i)\broot cause\b`,
		`(?i)\bthe (problem|issue|bug) (is|was)\b`,
		`(?i)\bturns out\b.*\b(was|because|due to)\b`,
		`(?i)\bthe fix (is|was)\b`,
		`(?i)\bworkaround\b`,
		`(?i)\bthat'?s why\b`,
		`(?i)\bthe reason it\b`,
		`(?i)\bfixed (it |the |by )\b`,
		`(?i)\bsolution (is|was)\b`,
		`(?i)\bresolved\b`,
		`(?i)\bpatched\b`,
		`(?i)\bthe answer (is|was)\b`,
		`(?i)\b(had|need) to\b.*\binstead\b`,
	}

	EmotionMarkers = []string{
		`(?i)\blove\b`,
		`(?i)\bscared\b`,
		`(?i)\bafraid\b`,
		`(?i)\bproud\b`,
		`(?i)\bhurt\b`,
		`(?i)\bhappy\b`,
		`(?i)\bsad\b`,
		`(?i)\bcry\b`,
		`(?i)\bcrying\b`,
		`(?i)\bmiss\b`,
		`(?i)\bsorry\b`,
		`(?i)\bgrateful\b`,
		`(?i)\bangry\b`,
		`(?i)\bworried\b`,
		`(?i)\blonely\b`,
		`(?i)\bbeautiful\b`,
		`(?i)\bamazing\b`,
		`(?i)\bwonderful\b`,
		`(?i)i feel`,
		`(?i)i'm scared`,
		`(?i)i love you`,
		`(?i)i'm sorry`,
		`(?i)i can't`,
		`(?i)i wish`,
		`(?i)i miss`,
		`(?i)i need`,
		`(?i)never told anyone`,
		`(?i)nobody knows`,
		`\*[^*]+\*`,
	}

	AllMarkers = map[string][]string{
		"decision":   DecisionMarkers,
		"preference": PreferenceMarkers,
		"milestone":  MilestoneMarkers,
		"problem":    ProblemMarkers,
		"emotional":  EmotionMarkers,
	}
)

// Compiled marker regexes — built at init time, never per-call.
var compiledMarkers map[string][]*regexp.Regexp

func init() {
	compiledMarkers = make(map[string][]*regexp.Regexp, len(AllMarkers))
	for mtype, markers := range AllMarkers {
		compiled := make([]*regexp.Regexp, len(markers))
		for i, pat := range markers {
			compiled[i] = regexp.MustCompile(pat)
		}
		compiledMarkers[mtype] = compiled
	}
}

// Sentiment word sets — ported from general_extractor.py:176-237.
var (
	positiveWords = map[string]bool{
		"pride": true, "proud": true, "joy": true, "happy": true, "love": true,
		"loving": true, "beautiful": true, "amazing": true, "wonderful": true,
		"incredible": true, "fantastic": true, "brilliant": true, "perfect": true,
		"excited": true, "thrilled": true, "grateful": true, "warm": true,
		"breakthrough": true, "success": true, "works": true, "working": true,
		"solved": true, "fixed": true, "nailed": true, "heart": true, "hug": true,
		"precious": true, "adore": true,
	}

	negativeWords = map[string]bool{
		"bug": true, "error": true, "crash": true, "crashing": true, "crashed": true,
		"fail": true, "failed": true, "failing": true, "failure": true, "broken": true,
		"broke": true, "breaking": true, "breaks": true, "issue": true, "problem": true,
		"wrong": true, "stuck": true, "blocked": true, "unable": true, "impossible": true,
		"missing": true, "terrible": true, "horrible": true, "awful": true, "worse": true,
		"worst": true, "panic": true, "disaster": true, "mess": true,
	}
)

var wordRe = regexp.MustCompile(`(?i)\b\w+\b`)

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

// Resolution patterns compiled at init.
var resolutionPatterns []*regexp.Regexp

func init() {
	pats := []string{
		`(?i)\bfixed\b`,
		`(?i)\bsolved\b`,
		`(?i)\bresolved\b`,
		`(?i)\bpatched\b`,
		`(?i)\bgot it working\b`,
		`(?i)\bit works\b`,
		`(?i)\bnailed it\b`,
		`(?i)\bfigured (it )?out\b`,
		`(?i)\bthe (fix|answer|solution)\b`,
	}
	for _, p := range pats {
		resolutionPatterns = append(resolutionPatterns, regexp.MustCompile(p))
	}
}

func hasResolution(text string) bool {
	lower := strings.ToLower(text)
	for _, re := range resolutionPatterns {
		if re.MatchString(lower) {
			return true
		}
	}
	return false
}

func disambiguate(memType, text string, scores map[string]float64) string {
	sentiment := getSentiment(text)

	if memType == "problem" && hasResolution(text) {
		if scores["emotional"] > 0 && sentiment == "positive" {
			return "emotional"
		}
		return "milestone"
	}

	if memType == "problem" && sentiment == "positive" {
		if scores["milestone"] > 0 {
			return "milestone"
		}
		if scores["emotional"] > 0 {
			return "emotional"
		}
	}

	return memType
}

// Code line detection — ported from general_extractor.py:293-307.
var codeLinePatterns []*regexp.Regexp

func init() {
	pats := []string{
		`^\s*[\$#]\s`,
		`^\s*(cd|source|echo|export|pip|npm|git|python|bash|curl|wget|mkdir|rm|cp|mv|ls|cat|grep|find|chmod|sudo|brew|docker)\s`,
		"^\\s*```",
		`^\s*(import|from|def|class|function|const|let|var|return)\s`,
		`^\s*[A-Z_]{2,}=`,
		`^\s*\|`,
		`^\s*[-]{2,}`,
		`^\s*[{}\[\]]\s*$`,
		`^\s*(if|for|while|try|except|elif|else:)\b`,
		`^\s*\w+\.\w+\(`,
		`^\s*\w+ = \w+\.\w+`,
	}
	for _, p := range pats {
		codeLinePatterns = append(codeLinePatterns, regexp.MustCompile(p))
	}
}

// IsCodeLine returns true if the line looks like code rather than prose.
func IsCodeLine(line string) bool {
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
	ratio := float64(alphaCount) / float64(max(utf8.RuneCountInString(stripped), 1))
	if ratio < 0.4 && len(stripped) > 10 {
		return true
	}
	return false
}

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
		if !IsCodeLine(line) {
			prose = append(prose, line)
		}
	}
	result := strings.TrimSpace(strings.Join(prose, "\n"))
	if result == "" {
		return text
	}
	return result
}

func scoreMarkers(text string, compiled []*regexp.Regexp) float64 {
	lower := strings.ToLower(text)
	score := 0.0
	for _, re := range compiled {
		matches := re.FindAllString(lower, -1)
		score += float64(len(matches))
	}
	return score
}

// Turn-detection patterns for splitIntoSegments — compiled at init.
var turnPatterns []*regexp.Regexp

func init() {
	pats := []string{
		`^>\s`,
		`(?i)^(Human|User|Q)\s*:`,
		`(?i)^(Assistant|AI|A|Claude|ChatGPT)\s*:`,
	}
	for _, p := range pats {
		turnPatterns = append(turnPatterns, regexp.MustCompile(p))
	}
}

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
		var segments []string
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

// ExtractMemories extracts typed memories from text using heuristic scoring.
// Port of extract_memories in general_extractor.py:363-421.
func ExtractMemories(text string, minConfidence float64) []Memory {
	paragraphs := splitIntoSegments(text)
	var memories []Memory

	// Stable ordering for deterministic results.
	typeOrder := []string{"decision", "preference", "milestone", "problem", "emotional"}

	for _, para := range paragraphs {
		if len(strings.TrimSpace(para)) < 20 {
			continue
		}

		prose := extractProse(para)

		scores := make(map[string]float64)
		for _, mtype := range typeOrder {
			s := scoreMarkers(prose, compiledMarkers[mtype])
			if s > 0 {
				scores[mtype] = s
			}
		}

		if len(scores) == 0 {
			continue
		}

		lengthBonus := 0.0
		if len(para) > 500 {
			lengthBonus = 2
		} else if len(para) > 200 {
			lengthBonus = 1
		}

		// Find max type (stable: use typeOrder)
		maxType := ""
		maxScore := 0.0
		for _, mtype := range typeOrder {
			if s, ok := scores[mtype]; ok && s > maxScore {
				maxScore = s
				maxType = mtype
			}
		}

		maxScore += lengthBonus
		maxType = disambiguate(maxType, prose, scores)

		confidence := maxScore / 5.0
		if confidence > 1.0 {
			confidence = 1.0
		}
		if confidence < minConfidence {
			continue
		}

		memories = append(memories, Memory{
			Content:    strings.TrimSpace(para),
			MemoryType: maxType,
			ChunkIndex: len(memories),
		})
	}

	return memories
}
