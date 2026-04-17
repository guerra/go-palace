package entity

import (
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Package-level compiled regexes. NEVER compile inside Detect's hot path.
// Name-specific person/project regexes are still compiled per candidate
// inside Detect — this mirrors the Python oracle's _build_patterns.
var (
	// URLs: http(s)://<stuff> up to whitespace/terminator. Trailing punctuation
	// is trimmed by trimTrailingPunct after the regex match.
	urlRe = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"'` + "`" + `]+`)

	// Emails: RFC-ish but pragmatic. Heuristic, not validator.
	emailRe = regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)

	// ISO-8601 date (with optional time zone). The non-time variant is the
	// common calendar shape `YYYY-MM-DD`.
	dateISORe = regexp.MustCompile(
		`\b\d{4}-\d{2}-\d{2}(?:T\d{2}:\d{2}:\d{2}(?:Z|[+-]\d{2}:\d{2})?)?\b`)

	// Month-first natural-language date: "January 2, 2026" / "Jan 2 2026".
	dateMonthFirstRe = regexp.MustCompile(
		`\b(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|Jun(?:e)?|` +
			`Jul(?:y)?|Aug(?:ust)?|Sept?(?:ember)?|Oct(?:ober)?|Nov(?:ember)?|` +
			`Dec(?:ember)?)\s+\d{1,2}(?:,?\s+\d{4})?\b`)

	// Day-first natural-language date: "2 January 2026" / "15 Mar 2026".
	dateDayFirstRe = regexp.MustCompile(
		`\b\d{1,2}\s+(?:Jan(?:uary)?|Feb(?:ruary)?|Mar(?:ch)?|Apr(?:il)?|May|` +
			`Jun(?:e)?|Jul(?:y)?|Aug(?:ust)?|Sept?(?:ember)?|Oct(?:ober)?|` +
			`Nov(?:ember)?|Dec(?:ember)?)(?:\s+\d{4})?\b`)

	// Tool candidate: 2-30-char alphanumeric (+ _ / -) starting with letter.
	// Case-insensitive — we lowercase and check commonTools.
	toolCandidateRe = regexp.MustCompile(`\b[A-Za-z][A-Za-z0-9_\-]{1,29}\b`)

	// Capitalized proper-noun candidates with Unicode support. The person /
	// project classifier consumes these spans.
	//
	// Uses Unicode letter classes so names like "María" or "Søren" aren't
	// dropped. Go's regexp engine treats \b as ASCII-only, so we anchor on
	// non-letter boundaries with \P{L} instead and a leading-anchor OR `(?:^|...)`.
	//
	// The single-word pattern accepts a leading uppercase + one or more
	// letters (lowercase-preferred but uppercase tails are tolerated so
	// CamelCase project names like "ChromaDB" land in the candidate pool).
	unicodeSingleWordRe = regexp.MustCompile(
		`(?:^|[^\p{L}\p{N}])(\p{Lu}[\p{L}\p{N}]{1,29})(?:[^\p{L}\p{N}]|$)`)
	unicodeMultiWordRe = regexp.MustCompile(
		`(?:^|[^\p{L}\p{N}])(\p{Lu}\p{Ll}+(?:\s+\p{Lu}\p{Ll}+)+)(?:[^\p{L}\p{N}]|$)`)

	// Place multi-word candidate: "Mount Washington", "Hudson River"
	// (placeSuffix preceded by 1+ capitalized words). Built dynamically
	// from placeSuffixes at package init so the regex stays in sync.
	placeMultiWordRe = buildPlaceRegex()
)

// buildPlaceRegex compiles a regex that matches one or more capitalized
// Latin words followed (or preceded) by a placeSuffixes word. Example:
// "Mount Washington", "Hudson River". Rendered case-insensitive on the
// suffix so "mount" / "Mount" / "MOUNT" all match.
func buildPlaceRegex() *regexp.Regexp {
	suffixes := make([]string, 0, len(placeSuffixes))
	for s := range placeSuffixes {
		suffixes = append(suffixes, s)
	}
	sort.Strings(suffixes) // deterministic regex
	alt := strings.Join(suffixes, "|")
	// Pattern: <Cap word>+ <space> <suffix>  OR  <suffix> <space> <Cap word>+
	// The suffix portion is case-insensitive-aware via (?i:...).
	pattern := `(?:\p{Lu}\p{Ll}+(?:\s+\p{Lu}\p{Ll}+)*\s+(?i:` + alt + `)` +
		`|(?i:` + alt + `)\s+\p{Lu}\p{Ll}+(?:\s+\p{Lu}\p{Ll}+)*)`
	return regexp.MustCompile(pattern)
}

// Person/project scoring templates. Copied verbatim from
// internal/entity/detector.go so behaviour stays in lockstep. %s is replaced
// per-candidate via regexp.QuoteMeta.
var (
	pkgPersonVerbTemplates = []string{
		`\b%s\s+said\b`,
		`\b%s\s+asked\b`,
		`\b%s\s+told\b`,
		`\b%s\s+replied\b`,
		`\b%s\s+laughed\b`,
		`\b%s\s+smiled\b`,
		`\b%s\s+cried\b`,
		`\b%s\s+felt\b`,
		`\b%s\s+thinks?\b`,
		`\b%s\s+wants?\b`,
		`\b%s\s+loves?\b`,
		`\b%s\s+hates?\b`,
		`\b%s\s+knows?\b`,
		`\b%s\s+decided\b`,
		`\b%s\s+pushed\b`,
		`\b%s\s+wrote\b`,
		`\bhey\s+%s\b`,
		`\bthanks?\s+%s\b`,
		`\bhi\s+%s\b`,
		`\bdear\s+%s\b`,
	}
	pkgDialogueTemplates = []string{
		`(?mi)^>\s*%s[:\s]`,
		`(?mi)^%s:\s`,
		`(?mi)^\[%s\]`,
		`(?i)"%s\s+said`,
	}
	pkgProjectVerbTemplates = []string{
		`\bbuilding\s+%s\b`,
		`\bbuilt\s+%s\b`,
		`\bship(?:ping|ped)?\s+%s\b`,
		`\blaunch(?:ing|ed)?\s+%s\b`,
		`\bdeploy(?:ing|ed)?\s+%s\b`,
		`\binstall(?:ing|ed)?\s+%s\b`,
		`\bthe\s+%s\s+architecture\b`,
		`\bthe\s+%s\s+pipeline\b`,
		`\bthe\s+%s\s+system\b`,
		`\bthe\s+%s\s+repo\b`,
		`\b%s\s+v\d+\b`,
		`\b%s\.py\b`,
		`\b%s-core\b`,
		`\b%s-local\b`,
		`\bimport\s+%s\b`,
		`\bpip\s+install\s+%s\b`,
	}
)

// pkgStopwords narrows internal/entity's stopword list to natural-language
// noise. UI/technical verbs that hurt person detection in per-string mode
// have been dropped (click, press, save, load, download, upload, etc.).
//
//nolint:gochecknoglobals
var pkgStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"in": true, "on": true, "at": true, "to": true, "for": true, "of": true,
	"with": true, "by": true, "from": true, "as": true, "is": true, "was": true,
	"are": true, "were": true, "be": true, "been": true, "being": true,
	"have": true, "has": true, "had": true, "do": true, "does": true, "did": true,
	"will": true, "would": true, "could": true, "should": true, "may": true,
	"might": true, "must": true, "shall": true, "can": true,
	"this": true, "that": true, "these": true, "those": true,
	"it": true, "its": true, "they": true, "them": true, "their": true,
	"we": true, "our": true, "you": true, "your": true,
	"i": true, "my": true, "me": true, "he": true, "she": true, "his": true, "her": true,
	"who": true, "what": true, "when": true, "where": true, "why": true, "how": true, "which": true,
	"if": true, "then": true, "so": true, "not": true, "no": true, "yes": true,
	"ok": true, "okay": true, "just": true, "very": true, "really": true,
	"also": true, "already": true, "still": true, "even": true, "only": true,
	"here": true, "there": true, "now": true, "too": true,
	"up": true, "out": true, "about": true, "like": true,
	"hey": true, "hi": true, "hello": true, "thanks": true, "thank": true,
	"right": true, "let": true,
	"something": true, "nothing": true, "everything": true, "anything": true,
	"someone": true, "everyone": true, "anyone": true,
	"day": true, "week": true, "month": true, "year": true, "today": true,
	"tomorrow": true, "yesterday": true,
}

// entityScores holds the per-candidate person-vs-project tally.
type entityScores struct {
	personScore    int
	projectScore   int
	personSignals  []string
	projectSignals []string
}

// Detect surfaces every recognized entity occurrence in content. Returns
// a slice sorted by Offset ascending. Offsets are BYTE offsets into content:
// content[e.Offset:e.Offset+len(e.Name)] == e.Name.
//
// Detection priority (early-stop / claim-based dedup):
//
//	URL > Email > Date > Tool > Place > Person > Project
//
// A byte range once claimed by a higher-priority type is not considered by
// lower-priority passes. A candidate that classifies as "uncertain" still
// emits — callers can filter on Confidence if they want strong hits only.
func Detect(content string) []Entity {
	if content == "" {
		return nil
	}
	claimed := make([]bool, len(content))
	var out []Entity

	// 1. URL.
	for _, idx := range urlRe.FindAllStringIndex(content, -1) {
		start, end := idx[0], idx[1]
		// Trim trailing punctuation so "See https://x.y." doesn't capture the period.
		for end > start && isTrailingPunct(content[end-1]) {
			end--
		}
		if start >= end || isClaimed(claimed, start, end) {
			continue
		}
		name := content[start:end]
		out = append(out, Entity{
			Name:       name,
			Type:       TypeURL,
			Canonical:  canonicalURL(name),
			Confidence: 0.95,
			Offset:     start,
		})
		claim(claimed, start, end)
	}

	// 2. Email.
	for _, idx := range emailRe.FindAllStringIndex(content, -1) {
		start, end := idx[0], idx[1]
		if isClaimed(claimed, start, end) {
			continue
		}
		name := content[start:end]
		out = append(out, Entity{
			Name:       name,
			Type:       TypeEmail,
			Canonical:  canonicalEmail(name),
			Confidence: 0.95,
			Offset:     start,
		})
		claim(claimed, start, end)
	}

	// 3. Date (three shapes, ISO first).
	for _, re := range []*regexp.Regexp{dateISORe, dateMonthFirstRe, dateDayFirstRe} {
		conf := 0.95
		if re != dateISORe {
			conf = 0.80
		}
		for _, idx := range re.FindAllStringIndex(content, -1) {
			start, end := idx[0], idx[1]
			if isClaimed(claimed, start, end) {
				continue
			}
			name := content[start:end]
			out = append(out, Entity{
				Name:       name,
				Type:       TypeDate,
				Canonical:  canonicalDate(name),
				Confidence: conf,
				Offset:     start,
			})
			claim(claimed, start, end)
		}
	}

	// 4. Tool.
	for _, idx := range toolCandidateRe.FindAllStringIndex(content, -1) {
		start, end := idx[0], idx[1]
		if isClaimed(claimed, start, end) {
			continue
		}
		name := content[start:end]
		lower := strings.ToLower(name)
		if !commonTools[lower] {
			continue
		}
		out = append(out, Entity{
			Name:       name,
			Type:       TypeTool,
			Canonical:  lower,
			Confidence: 0.85,
			Offset:     start,
		})
		claim(claimed, start, end)
	}

	// 5. Place.
	for _, idx := range placeMultiWordRe.FindAllStringIndex(content, -1) {
		start, end := idx[0], idx[1]
		if isClaimed(claimed, start, end) {
			continue
		}
		name := content[start:end]
		out = append(out, Entity{
			Name:       name,
			Type:       TypePlace,
			Canonical:  name,
			Confidence: 0.70,
			Offset:     start,
		})
		claim(claimed, start, end)
	}

	// 6. Person + Project (shared candidate pass).
	candidates := extractCapitalizedCandidates(content, claimed)
	// Sort by offset so emission is deterministic.
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].offset != candidates[j].offset {
			return candidates[i].offset < candidates[j].offset
		}
		return candidates[i].name < candidates[j].name
	})
	// Dedupe on (name, offset) and cache per-unique-name scoring so the
	// per-candidate regex compile cost is paid once per name, not once per
	// occurrence. For a document with N occurrences of the same name, this
	// is an N→1 speedup on the person/project classification hot path.
	seenCand := make(map[string]bool, len(candidates))
	scoreCache := make(map[string]entityScores, len(candidates))
	lines := strings.Split(content, "\n")
	for _, c := range candidates {
		key := fmt.Sprintf("%d:%s", c.offset, c.name)
		if seenCand[key] {
			continue
		}
		seenCand[key] = true
		if isClaimed(claimed, c.offset, c.offset+len(c.name)) {
			continue
		}
		scores, ok := scoreCache[c.name]
		if !ok {
			scores = scorePersonProject(c.name, content, lines)
			scoreCache[c.name] = scores
		}
		e := classifyPersonProject(c.name, c.offset, scores)
		if e.Type == "" {
			continue
		}
		out = append(out, e)
		claim(claimed, c.offset, c.offset+len(c.name))
	}

	// Final sort: Offset ascending, Type ascending as tiebreaker.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Offset != out[j].Offset {
			return out[i].Offset < out[j].Offset
		}
		return out[i].Type < out[j].Type
	})
	// Dedup exact (Name, Type, Offset) triples.
	if len(out) == 0 {
		return nil
	}
	deduped := out[:0]
	seen := make(map[string]bool, len(out))
	for _, e := range out {
		k := fmt.Sprintf("%d|%s|%s", e.Offset, e.Type, e.Name)
		if seen[k] {
			continue
		}
		seen[k] = true
		deduped = append(deduped, e)
	}
	return deduped
}

// capCandidate is an internal struct: a capitalized span + byte offset.
type capCandidate struct {
	name   string
	offset int
}

// extractCapitalizedCandidates walks content and emits capitalized spans,
// preferring multi-word matches over the single-word forms they contain.
// Stopwords are filtered out (case-insensitive).
func extractCapitalizedCandidates(content string, claimed []bool) []capCandidate {
	var out []capCandidate

	// Multi-word first (higher priority).
	for _, idx := range unicodeMultiWordRe.FindAllStringSubmatchIndex(content, -1) {
		// Group 1 is the captured capitalized span (without the boundary chars).
		if len(idx) < 4 {
			continue
		}
		start, end := idx[2], idx[3]
		if start < 0 || end < 0 {
			continue
		}
		name := content[start:end]
		if containsStopword(name) {
			continue
		}
		if isClaimed(claimed, start, end) {
			continue
		}
		out = append(out, capCandidate{name: name, offset: start})
	}

	// Single-word.
	for _, idx := range unicodeSingleWordRe.FindAllStringSubmatchIndex(content, -1) {
		if len(idx) < 4 {
			continue
		}
		start, end := idx[2], idx[3]
		if start < 0 || end < 0 {
			continue
		}
		name := content[start:end]
		if pkgStopwords[strings.ToLower(name)] {
			continue
		}
		if isClaimed(claimed, start, end) {
			continue
		}
		out = append(out, capCandidate{name: name, offset: start})
	}
	return out
}

func containsStopword(span string) bool {
	for _, w := range strings.Fields(span) {
		if pkgStopwords[strings.ToLower(w)] {
			return true
		}
	}
	return false
}

// scorePersonProject builds per-candidate regexes and tallies signals. This
// is the per-call compile path — same as Python's _build_patterns — so we
// accept the cost in exchange for name-specific precision.
func scorePersonProject(name, content string, lines []string) entityScores {
	q := regexp.QuoteMeta(name)
	var scores entityScores

	// Dialogue markers (3x each).
	for _, tmpl := range pkgDialogueTemplates {
		rx := regexp.MustCompile(fmt.Sprintf(tmpl, q))
		n := len(rx.FindAllString(content, -1))
		if n > 0 {
			scores.personScore += n * 3
			scores.personSignals = append(scores.personSignals,
				fmt.Sprintf("dialogue (%dx)", n))
		}
	}

	// Person verbs (2x).
	for _, tmpl := range pkgPersonVerbTemplates {
		rx := regexp.MustCompile("(?i)" + fmt.Sprintf(tmpl, q))
		n := len(rx.FindAllString(content, -1))
		if n > 0 {
			scores.personScore += n * 2
			scores.personSignals = append(scores.personSignals,
				fmt.Sprintf("action (%dx)", n))
		}
	}

	// Pronoun proximity (2x per line-adjacent pronoun). Window = ±2 lines.
	nameLower := strings.ToLower(name)
	var nameLineIdx []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), nameLower) {
			nameLineIdx = append(nameLineIdx, i)
		}
	}
	pronounRxes := []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bshe\b`),
		regexp.MustCompile(`(?i)\bhe\b`),
		regexp.MustCompile(`(?i)\bher\b`),
		regexp.MustCompile(`(?i)\bhim\b`),
		regexp.MustCompile(`(?i)\bhis\b`),
		regexp.MustCompile(`(?i)\bhers\b`),
		regexp.MustCompile(`(?i)\bthey\b`),
		regexp.MustCompile(`(?i)\bthem\b`),
		regexp.MustCompile(`(?i)\btheir\b`),
	}
	hits := 0
	for _, li := range nameLineIdx {
		start := li - 2
		if start < 0 {
			start = 0
		}
		end := li + 3
		if end > len(lines) {
			end = len(lines)
		}
		window := strings.ToLower(strings.Join(lines[start:end], " "))
		for _, rx := range pronounRxes {
			if rx.MatchString(window) {
				hits++
				break
			}
		}
	}
	if hits > 0 {
		scores.personScore += hits * 2
		scores.personSignals = append(scores.personSignals,
			fmt.Sprintf("pronoun (%dx)", hits))
	}

	// Direct address (4x).
	direct := regexp.MustCompile(
		"(?i)" + fmt.Sprintf(`\bhey\s+%s\b|\bthanks?\s+%s\b|\bhi\s+%s\b`, q, q, q))
	if n := len(direct.FindAllString(content, -1)); n > 0 {
		scores.personScore += n * 4
		scores.personSignals = append(scores.personSignals,
			fmt.Sprintf("addressed (%dx)", n))
	}

	// Project verbs (2x).
	for _, tmpl := range pkgProjectVerbTemplates {
		rx := regexp.MustCompile("(?i)" + fmt.Sprintf(tmpl, q))
		n := len(rx.FindAllString(content, -1))
		if n > 0 {
			scores.projectScore += n * 2
			scores.projectSignals = append(scores.projectSignals,
				fmt.Sprintf("project-verb (%dx)", n))
		}
	}

	// Versioned / hyphenated (3x).
	versioned := regexp.MustCompile(
		"(?i)" + fmt.Sprintf(`\b%s[-v]\w+`, q))
	if n := len(versioned.FindAllString(content, -1)); n > 0 {
		scores.projectScore += n * 3
		scores.projectSignals = append(scores.projectSignals,
			fmt.Sprintf("versioned (%dx)", n))
	}

	// Code file ref (3x).
	codeRef := regexp.MustCompile(
		"(?i)" + fmt.Sprintf(`\b%s\.(?:py|js|ts|yaml|yml|json|sh|go|rs)\b`, q))
	if n := len(codeRef.FindAllString(content, -1)); n > 0 {
		scores.projectScore += n * 3
		scores.projectSignals = append(scores.projectSignals,
			fmt.Sprintf("code-ref (%dx)", n))
	}
	return scores
}

// classifyPersonProject applies the Python scoring rules and returns an
// Entity (or the zero Entity if the candidate has no signal at all — those
// are dropped by the caller).
func classifyPersonProject(name string, offset int, s entityScores) Entity {
	total := s.personScore + s.projectScore
	if total == 0 {
		// No signals on this candidate — do not emit. Avoids flooding output
		// with every random capitalized word.
		return Entity{}
	}
	// Count distinct person-signal categories.
	cats := map[string]bool{}
	for _, sig := range s.personSignals {
		switch {
		case strings.HasPrefix(sig, "dialogue"):
			cats["dialogue"] = true
		case strings.HasPrefix(sig, "action"):
			cats["action"] = true
		case strings.HasPrefix(sig, "pronoun"):
			cats["pronoun"] = true
		case strings.HasPrefix(sig, "addressed"):
			cats["addressed"] = true
		}
	}
	twoCats := len(cats) >= 2
	personRatio := float64(s.personScore) / float64(total)

	switch {
	case personRatio >= 0.7 && twoCats && s.personScore >= 5:
		return Entity{
			Name:       name,
			Type:       TypePerson,
			Canonical:  name,
			Confidence: clamp(0.5+personRatio*0.5, 0.5, 0.99),
			Offset:     offset,
		}
	case personRatio <= 0.3 && s.projectScore > 0:
		return Entity{
			Name:       name,
			Type:       TypeProject,
			Canonical:  name,
			Confidence: clamp(0.5+(1-personRatio)*0.5, 0.5, 0.99),
			Offset:     offset,
		}
	default:
		return Entity{
			Name:       name,
			Type:       TypeUncertain,
			Canonical:  name,
			Confidence: 0.4,
			Offset:     offset,
		}
	}
}

func clamp(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// claim marks [start, end) as taken.
func claim(claimed []bool, start, end int) {
	if start < 0 {
		start = 0
	}
	if end > len(claimed) {
		end = len(claimed)
	}
	for i := start; i < end; i++ {
		claimed[i] = true
	}
}

// isClaimed reports whether any byte in [start, end) is already claimed.
func isClaimed(claimed []bool, start, end int) bool {
	if start < 0 {
		start = 0
	}
	if end > len(claimed) {
		end = len(claimed)
	}
	for i := start; i < end; i++ {
		if claimed[i] {
			return true
		}
	}
	return false
}

func isTrailingPunct(b byte) bool {
	switch b {
	case '.', ',', '!', '?', ';', ':', ')', ']', '}', '"', '\'':
		return true
	}
	return false
}

// canonicalURL lowercases scheme + host, preserves the rest.
// Falls through to the raw string on parse error.
func canonicalURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)
	return u.String()
}

// canonicalEmail lowercases both local and domain.
// Falls through to raw-lowercase on parse error.
func canonicalEmail(raw string) string {
	a, err := mail.ParseAddress(raw)
	if err != nil {
		return strings.ToLower(raw)
	}
	return strings.ToLower(a.Address)
}

// canonicalDate re-formats to ISO 8601 "2006-01-02" when parseable, else raw.
func canonicalDate(raw string) string {
	layouts := []string{
		time.RFC3339,
		"2006-01-02",
		"January 2, 2006",
		"January 2 2006",
		"Jan 2, 2006",
		"Jan 2 2006",
		"2 January 2006",
		"2 Jan 2006",
	}
	for _, l := range layouts {
		if t, err := time.Parse(l, raw); err == nil {
			return t.Format("2006-01-02")
		}
	}
	return raw
}
