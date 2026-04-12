// Package entity implements entity detection (people and projects from text)
// and a persistent entity registry. Ports entity_detector.py + entity_registry.py.
package entity

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Signal patterns — compiled at init from template strings.
var personVerbTemplates = []string{
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

var pronounPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\bshe\b`),
	regexp.MustCompile(`(?i)\bher\b`),
	regexp.MustCompile(`(?i)\bhers\b`),
	regexp.MustCompile(`(?i)\bhe\b`),
	regexp.MustCompile(`(?i)\bhim\b`),
	regexp.MustCompile(`(?i)\bhis\b`),
	regexp.MustCompile(`(?i)\bthey\b`),
	regexp.MustCompile(`(?i)\bthem\b`),
	regexp.MustCompile(`(?i)\btheir\b`),
}

var dialogueTemplates = []string{
	`(?mi)^>\s*%s[:\s]`,
	`(?mi)^%s:\s`,
	`(?mi)^\[%s\]`,
	`(?i)"%s\s+said`,
}

var projectVerbTemplates = []string{
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

//nolint:gochecknoglobals
var stopwords = map[string]bool{
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
	"use": true, "get": true, "got": true, "make": true, "made": true,
	"take": true, "put": true, "come": true, "go": true, "see": true,
	"know": true, "think": true, "true": true, "false": true,
	"none": true, "null": true, "new": true, "old": true,
	"all": true, "any": true, "some": true,
	"return": true, "print": true, "def": true, "class": true, "import": true,
	"step": true, "usage": true, "run": true, "check": true, "find": true,
	"add": true, "set": true, "list": true, "args": true, "dict": true,
	"str": true, "int": true, "bool": true, "path": true, "file": true,
	"type": true, "name": true, "note": true, "example": true,
	"option": true, "result": true, "error": true, "warning": true, "info": true,
	"every": true, "each": true, "more": true, "less": true,
	"next": true, "last": true, "first": true, "second": true,
	"stack": true, "layer": true, "mode": true, "test": true,
	"stop": true, "start": true, "copy": true, "move": true,
	"source": true, "target": true, "output": true, "input": true,
	"data": true, "item": true, "key": true, "value": true,
	"returns": true, "raises": true, "yields": true, "self": true, "cls": true, "kwargs": true,
	"world": true, "well": true, "want": true, "topic": true,
	"choose": true, "social": true, "cars": true, "phones": true,
	"healthcare": true, "ex": true, "machina": true, "deus": true,
	"human": true, "humans": true, "people": true, "things": true,
	"something": true, "nothing": true, "everything": true, "anything": true,
	"someone": true, "everyone": true, "anyone": true,
	"way": true, "time": true, "day": true, "life": true,
	"place": true, "thing": true, "part": true, "kind": true,
	"sort": true, "case": true, "point": true, "idea": true,
	"fact": true, "sense": true, "question": true, "answer": true,
	"reason": true, "number": true, "version": true, "system": true,
	"hey": true, "hi": true, "hello": true, "thanks": true, "thank": true,
	"right": true, "let": true,
	"click": true, "hit": true, "press": true, "tap": true,
	"drag": true, "drop": true, "open": true, "close": true,
	"save": true, "load": true, "launch": true, "install": true,
	"download": true, "upload": true, "scroll": true, "select": true,
	"enter": true, "submit": true, "cancel": true, "confirm": true,
	"delete": true, "paste": true, "write": true, "read": true,
	"search": true, "show": true, "hide": true,
	"desktop": true, "documents": true, "downloads": true, "users": true,
	"home": true, "library": true, "applications": true,
	"preferences": true, "settings": true, "terminal": true,
	"actor": true, "vector": true, "remote": true, "control": true,
	"duration": true, "fetch": true,
	"agents": true, "tools": true, "others": true, "guards": true,
	"ethics": true, "regulation": true, "learning": true, "thinking": true,
	"memory": true, "language": true, "intelligence": true, "technology": true,
	"society": true, "culture": true, "future": true, "history": true,
	"science": true, "model": true, "models": true, "network": true,
	"networks": true, "training": true, "inference": true,
}

var (
	proseExtensions = map[string]bool{
		".txt": true, ".md": true, ".rst": true, ".csv": true,
	}
	readableExtensions = map[string]bool{
		".txt": true, ".md": true, ".py": true, ".js": true, ".ts": true,
		".json": true, ".yaml": true, ".yml": true, ".csv": true, ".rst": true,
		".toml": true, ".sh": true, ".rb": true, ".go": true, ".rs": true,
	}
	skipDirs = map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "venv": true, "env": true, "dist": true,
		"build": true, ".next": true, "coverage": true, ".mempalace": true,
	}
)

const maxBytesPerFile = 5000

var (
	singleWordRe = regexp.MustCompile(`\b([A-Z][a-z]{1,19})\b`)
	multiWordRe  = regexp.MustCompile(`\b([A-Z][a-z]+(?:\s+[A-Z][a-z]+)+)\b`)
)

// Entity is a detected entity candidate.
type Entity struct {
	Name       string   `json:"name"`
	Type       string   `json:"type"` // person, project, uncertain
	Confidence float64  `json:"confidence"`
	Frequency  int      `json:"frequency"`
	Signals    []string `json:"signals"`
}

// EntityScores holds the scoring result for a candidate.
type EntityScores struct {
	PersonScore    int
	ProjectScore   int
	PersonSignals  []string
	ProjectSignals []string
}

// DetectionResult holds categorised entities.
type DetectionResult struct {
	People    []Entity `json:"people"`
	Projects  []Entity `json:"projects"`
	Uncertain []Entity `json:"uncertain"`
}

// ExtractCandidates finds capitalized proper noun candidates in text.
// Returns name→frequency for names appearing 3+ times.
func ExtractCandidates(text string) map[string]int {
	counts := map[string]int{}

	for _, match := range singleWordRe.FindAllString(text, -1) {
		if len(match) > 1 && !stopwords[strings.ToLower(match)] {
			counts[match]++
		}
	}

	for _, match := range multiWordRe.FindAllString(text, -1) {
		words := strings.Fields(match)
		skip := false
		for _, w := range words {
			if stopwords[strings.ToLower(w)] {
				skip = true
				break
			}
		}
		if !skip {
			counts[match]++
		}
	}

	filtered := map[string]int{}
	for name, count := range counts {
		if count >= 3 {
			filtered[name] = count
		}
	}
	return filtered
}

// buildPatterns compiles regex patterns for a specific entity name.
func buildPatterns(name string) map[string]interface{} {
	n := regexp.QuoteMeta(name)

	dialogue := make([]*regexp.Regexp, 0, len(dialogueTemplates))
	for _, tmpl := range dialogueTemplates {
		dialogue = append(dialogue, regexp.MustCompile(fmt.Sprintf(tmpl, n)))
	}

	personVerbs := make([]*regexp.Regexp, 0, len(personVerbTemplates))
	for _, tmpl := range personVerbTemplates {
		personVerbs = append(personVerbs, regexp.MustCompile("(?i)"+fmt.Sprintf(tmpl, n)))
	}

	projectVerbs := make([]*regexp.Regexp, 0, len(projectVerbTemplates))
	for _, tmpl := range projectVerbTemplates {
		projectVerbs = append(projectVerbs, regexp.MustCompile("(?i)"+fmt.Sprintf(tmpl, n)))
	}

	direct := regexp.MustCompile("(?i)" + fmt.Sprintf(`\bhey\s+%s\b|\bthanks?\s+%s\b|\bhi\s+%s\b`, n, n, n))
	versioned := regexp.MustCompile("(?i)" + fmt.Sprintf(`\b%s[-v]\w+`, n))
	codeRef := regexp.MustCompile("(?i)" + fmt.Sprintf(`\b%s\.(py|js|ts|yaml|yml|json|sh)\b`, n))

	return map[string]interface{}{
		"dialogue":      dialogue,
		"person_verbs":  personVerbs,
		"project_verbs": projectVerbs,
		"direct":        direct,
		"versioned":     versioned,
		"code_ref":      codeRef,
	}
}

// ScoreEntity scores a candidate as person vs project.
func ScoreEntity(name, text string, lines []string) EntityScores {
	patterns := buildPatterns(name)
	var personScore, projectScore int
	var personSignals, projectSignals []string

	// Dialogue markers (3x)
	for _, rx := range patterns["dialogue"].([]*regexp.Regexp) {
		matches := len(rx.FindAllString(text, -1))
		if matches > 0 {
			personScore += matches * 3
			personSignals = append(personSignals, fmt.Sprintf("dialogue marker (%dx)", matches))
		}
	}

	// Person verbs (2x)
	for _, rx := range patterns["person_verbs"].([]*regexp.Regexp) {
		matches := len(rx.FindAllString(text, -1))
		if matches > 0 {
			personScore += matches * 2
			personSignals = append(personSignals, fmt.Sprintf("'%s ...' action (%dx)", name, matches))
		}
	}

	// Pronoun proximity (2x within 2 lines before + 2 after)
	nameLower := strings.ToLower(name)
	var nameLineIndices []int
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), nameLower) {
			nameLineIndices = append(nameLineIndices, i)
		}
	}
	pronounHits := 0
	for _, idx := range nameLineIndices {
		start := idx - 2
		if start < 0 {
			start = 0
		}
		end := idx + 3
		if end > len(lines) {
			end = len(lines)
		}
		windowText := strings.ToLower(strings.Join(lines[start:end], " "))
		for _, rx := range pronounPatterns {
			if rx.MatchString(windowText) {
				pronounHits++
				break
			}
		}
	}
	if pronounHits > 0 {
		personScore += pronounHits * 2
		personSignals = append(personSignals, fmt.Sprintf("pronoun nearby (%dx)", pronounHits))
	}

	// Direct address (4x)
	direct := patterns["direct"].(*regexp.Regexp)
	directCount := len(direct.FindAllString(text, -1))
	if directCount > 0 {
		personScore += directCount * 4
		personSignals = append(personSignals, fmt.Sprintf("addressed directly (%dx)", directCount))
	}

	// Project verbs (2x)
	for _, rx := range patterns["project_verbs"].([]*regexp.Regexp) {
		matches := len(rx.FindAllString(text, -1))
		if matches > 0 {
			projectScore += matches * 2
			projectSignals = append(projectSignals, fmt.Sprintf("project verb (%dx)", matches))
		}
	}

	// Versioned refs (3x)
	versioned := patterns["versioned"].(*regexp.Regexp)
	versionedCount := len(versioned.FindAllString(text, -1))
	if versionedCount > 0 {
		projectScore += versionedCount * 3
		projectSignals = append(projectSignals, fmt.Sprintf("versioned/hyphenated (%dx)", versionedCount))
	}

	// Code refs (3x)
	codeRef := patterns["code_ref"].(*regexp.Regexp)
	codeRefCount := len(codeRef.FindAllString(text, -1))
	if codeRefCount > 0 {
		projectScore += codeRefCount * 3
		projectSignals = append(projectSignals, fmt.Sprintf("code file reference (%dx)", codeRefCount))
	}

	// Cap signals at 3 each
	if len(personSignals) > 3 {
		personSignals = personSignals[:3]
	}
	if len(projectSignals) > 3 {
		projectSignals = projectSignals[:3]
	}

	return EntityScores{
		PersonScore:    personScore,
		ProjectScore:   projectScore,
		PersonSignals:  personSignals,
		ProjectSignals: projectSignals,
	}
}

// ClassifyEntity classifies based on scores. Requires 2+ signal categories for person.
func ClassifyEntity(name string, frequency int, scores EntityScores) Entity {
	ps := scores.PersonScore
	prs := scores.ProjectScore
	total := ps + prs

	if total == 0 {
		confidence := float64(frequency) / 50
		if confidence > 0.4 {
			confidence = 0.4
		}
		return Entity{
			Name:       name,
			Type:       "uncertain",
			Confidence: roundTo2(confidence),
			Frequency:  frequency,
			Signals:    []string{fmt.Sprintf("appears %dx, no strong type signals", frequency)},
		}
	}

	personRatio := float64(ps) / float64(total)

	// Count signal categories
	categories := map[string]bool{}
	for _, s := range scores.PersonSignals {
		switch {
		case strings.Contains(s, "dialogue"):
			categories["dialogue"] = true
		case strings.Contains(s, "action"):
			categories["action"] = true
		case strings.Contains(s, "pronoun"):
			categories["pronoun"] = true
		case strings.Contains(s, "addressed"):
			categories["addressed"] = true
		}
	}
	hasTwoSignalTypes := len(categories) >= 2

	var entityType string
	var confidence float64
	var signals []string

	switch {
	case personRatio >= 0.7 && hasTwoSignalTypes && ps >= 5:
		entityType = "person"
		confidence = 0.5 + personRatio*0.5
		if confidence > 0.99 {
			confidence = 0.99
		}
		signals = scores.PersonSignals
		if len(signals) == 0 {
			signals = []string{fmt.Sprintf("appears %dx", frequency)}
		}
	case personRatio >= 0.7 && (!hasTwoSignalTypes || ps < 5):
		entityType = "uncertain"
		confidence = 0.4
		signals = make([]string, len(scores.PersonSignals))
		copy(signals, scores.PersonSignals)
		signals = append(signals, fmt.Sprintf("appears %dx — pronoun-only match", frequency))
	case personRatio <= 0.3:
		entityType = "project"
		confidence = 0.5 + (1-personRatio)*0.5
		if confidence > 0.99 {
			confidence = 0.99
		}
		signals = scores.ProjectSignals
		if len(signals) == 0 {
			signals = []string{fmt.Sprintf("appears %dx", frequency)}
		}
	default:
		entityType = "uncertain"
		confidence = 0.5
		signals = make([]string, len(scores.PersonSignals))
		copy(signals, scores.PersonSignals)
		signals = append(signals, scores.ProjectSignals...)
		if len(signals) > 3 {
			signals = signals[:3]
		}
		signals = append(signals, "mixed signals — needs review")
	}

	return Entity{
		Name:       name,
		Type:       entityType,
		Confidence: roundTo2(confidence),
		Frequency:  frequency,
		Signals:    signals,
	}
}

// Detect scans files and classifies entity candidates.
func Detect(filePaths []string, maxFiles int) DetectionResult {
	var allText []string
	var allLines []string
	filesRead := 0

	for _, fp := range filePaths {
		if filesRead >= maxFiles {
			break
		}
		data, err := os.ReadFile(fp)
		if err != nil {
			continue
		}
		content := string(data)
		if len(content) > maxBytesPerFile {
			content = content[:maxBytesPerFile]
		}
		allText = append(allText, content)
		allLines = append(allLines, strings.Split(content, "\n")...)
		filesRead++
	}

	combinedText := strings.Join(allText, "\n")
	candidates := ExtractCandidates(combinedText)

	if len(candidates) == 0 {
		return DetectionResult{}
	}

	// Sort candidates by frequency desc for deterministic processing
	type nameFreq struct {
		name string
		freq int
	}
	sorted := make([]nameFreq, 0, len(candidates))
	for name, freq := range candidates {
		sorted = append(sorted, nameFreq{name, freq})
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].freq != sorted[j].freq {
			return sorted[i].freq > sorted[j].freq
		}
		return sorted[i].name < sorted[j].name
	})

	var people, projects, uncertain []Entity
	for _, nf := range sorted {
		scores := ScoreEntity(nf.name, combinedText, allLines)
		entity := ClassifyEntity(nf.name, nf.freq, scores)

		switch entity.Type {
		case "person":
			people = append(people, entity)
		case "project":
			projects = append(projects, entity)
		default:
			uncertain = append(uncertain, entity)
		}
	}

	sort.Slice(people, func(i, j int) bool { return people[i].Confidence > people[j].Confidence })
	sort.Slice(projects, func(i, j int) bool { return projects[i].Confidence > projects[j].Confidence })
	sort.Slice(uncertain, func(i, j int) bool { return uncertain[i].Frequency > uncertain[j].Frequency })

	if len(people) > 15 {
		people = people[:15]
	}
	if len(projects) > 10 {
		projects = projects[:10]
	}
	if len(uncertain) > 8 {
		uncertain = uncertain[:8]
	}

	return DetectionResult{
		People:    people,
		Projects:  projects,
		Uncertain: uncertain,
	}
}

// ScanForDetection collects prose file paths for entity detection.
func ScanForDetection(projectDir string, maxFiles int) ([]string, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, err
	}

	var proseFiles, allFiles []string
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] && path != absDir {
				return filepath.SkipDir
			}
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if proseExtensions[ext] {
			proseFiles = append(proseFiles, path)
		} else if readableExtensions[ext] {
			allFiles = append(allFiles, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	var files []string
	if len(proseFiles) >= 3 {
		files = proseFiles
	} else {
		files = make([]string, len(proseFiles))
		copy(files, proseFiles)
		files = append(files, allFiles...)
	}
	if len(files) > maxFiles {
		files = files[:maxFiles]
	}
	return files, nil
}

// Confirm displays detected entities and confirms with the user.
// If yes=true, auto-accepts (skips uncertain).
func Confirm(detected DetectionResult, yes bool, w io.Writer, _ io.Reader) (people []string, projects []string) {
	bar := strings.Repeat("=", 58)

	fmt.Fprintf(w, "\n%s\n  MemPalace — Entity Detection\n%s\n", bar, bar)
	fmt.Fprintf(w, "\n  Scanned your files. Here's what we found:\n")

	printEntityList(w, detected.People, "PEOPLE")
	printEntityList(w, detected.Projects, "PROJECTS")

	if len(detected.Uncertain) > 0 {
		printEntityList(w, detected.Uncertain, "UNCERTAIN (need your call)")
	}

	for _, e := range detected.People {
		people = append(people, e.Name)
	}
	for _, e := range detected.Projects {
		projects = append(projects, e.Name)
	}

	if yes {
		fmt.Fprintf(w, "\n  Auto-accepting %d people, %d projects.\n", len(people), len(projects))
		return people, projects
	}

	// Non-interactive fallback: auto-accept for now (interactive flow deferred)
	fmt.Fprintf(w, "\n  Auto-accepting %d people, %d projects.\n", len(people), len(projects))
	return people, projects
}

func printEntityList(w io.Writer, entities []Entity, label string) {
	fmt.Fprintf(w, "\n  %s:\n", label)
	if len(entities) == 0 {
		fmt.Fprintf(w, "    (none detected)\n")
		return
	}
	for i, e := range entities {
		filled := int(e.Confidence * 5)
		if filled > 5 {
			filled = 5
		}
		bar := strings.Repeat("*", filled) + strings.Repeat(".", 5-filled)
		signalStr := ""
		if len(e.Signals) > 0 {
			limit := 2
			if len(e.Signals) < limit {
				limit = len(e.Signals)
			}
			signalStr = strings.Join(e.Signals[:limit], ", ")
		}
		fmt.Fprintf(w, "    %2d. %-20s [%s] %s\n", i+1, e.Name, bar, signalStr)
	}
}

// SaveEntities writes entities.json to the project directory.
func SaveEntities(dir string, people, projects []string) error {
	if people == nil {
		people = []string{}
	}
	if projects == nil {
		projects = []string{}
	}
	data := map[string]interface{}{
		"people":   people,
		"projects": projects,
	}
	b, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("entity: marshal: %w", err)
	}
	path := filepath.Join(dir, "entities.json")
	return os.WriteFile(path, append(b, '\n'), 0o644)
}

func roundTo2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
