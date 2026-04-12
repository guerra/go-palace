package entity

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

//nolint:gochecknoglobals
var commonEnglishWords = map[string]bool{
	"ever": true, "grace": true, "will": true, "bill": true,
	"mark": true, "april": true, "may": true, "june": true,
	"joy": true, "hope": true, "faith": true, "chance": true,
	"chase": true, "hunter": true, "dash": true, "flash": true,
	"star": true, "sky": true, "river": true, "brook": true,
	"lane": true, "art": true, "clay": true, "gil": true,
	"nat": true, "max": true, "rex": true, "ray": true,
	"jay": true, "rose": true, "violet": true, "lily": true,
	"ivy": true, "ash": true, "reed": true, "sage": true,
	"monday": true, "tuesday": true, "wednesday": true,
	"thursday": true, "friday": true, "saturday": true, "sunday": true,
	"january": true, "february": true, "march": true,
	"july": true, "august": true, "september": true,
	"october": true, "november": true, "december": true,
}

var personContextTemplates = []string{
	`\b%s\s+said\b`, `\b%s\s+told\b`, `\b%s\s+asked\b`,
	`\b%s\s+laughed\b`, `\b%s\s+smiled\b`,
	`\b%s\s+was\b`, `\b%s\s+is\b`,
	`\b%s\s+called\b`, `\b%s\s+texted\b`,
	`\bwith\s+%s\b`, `\bsaw\s+%s\b`,
	`\bcalled\s+%s\b`, `\btook\s+%s\b`,
	`\bpicked\s+up\s+%s\b`,
	`\bdrop(?:ped)?\s+(?:off\s+)?%s\b`,
	`\b%s(?:'s|s')\b`,
	`\bhey\s+%s\b`, `\bthanks?\s+%s\b`,
	`(?m)^%s[:\s]`,
	`\bmy\s+(?:son|daughter|kid|child|brother|sister|friend|partner|colleague|coworker)\s+%s\b`,
}

var conceptContextTemplates = []string{
	`\bhave\s+you\s+%s\b`, `\bif\s+you\s+%s\b`,
	`\b%s\s+since\b`, `\b%s\s+again\b`,
	`\bnot\s+%s\b`, `\b%s\s+more\b`,
	`\bwould\s+%s\b`, `\bcould\s+%s\b`,
	`\bwill\s+%s\b`,
	`(?:the\s+)?%s\s+(?:of|in|at|for|to)\b`,
}

var nameIndicatorPhrases = []string{
	"given name", "personal name", "first name", "forename",
	"masculine name", "feminine name", "boy's name", "girl's name",
	"male name", "female name", "irish name", "welsh name",
	"scottish name", "gaelic name", "hebrew name", "arabic name",
	"norse name", "old english name", "is a name", "as a name",
	"name meaning", "name derived from",
	"legendary irish", "legendary welsh", "legendary scottish",
}

var placeIndicatorPhrases = []string{
	"city in", "town in", "village in", "municipality",
	"capital of", "district of", "county", "province",
	"region of", "island of", "mountain in", "river in",
}

// PersonInfo stores registry data about a known person.
type PersonInfo struct {
	Source       string   `json:"source"`
	Relationship string   `json:"relationship"`
	Contexts     []string `json:"contexts"`
	Aliases      []string `json:"aliases"`
	Confidence   float64  `json:"confidence"`
	Canonical    string   `json:"canonical,omitempty"`
	SeenCount    int      `json:"seen_count,omitempty"`
}

// WikiResult stores a Wikipedia research result.
type WikiResult struct {
	InferredType string  `json:"inferred_type"`
	Confidence   float64 `json:"confidence"`
	WikiSummary  string  `json:"wiki_summary,omitempty"`
	WikiTitle    string  `json:"wiki_title,omitempty"`
	Word         string  `json:"word,omitempty"`
	Note         string  `json:"note,omitempty"`
	Confirmed    bool    `json:"confirmed"`
}

// LookupResult is the result of looking up a word.
type LookupResult struct {
	Type                string   `json:"type"`
	Source              string   `json:"source"`
	Name                string   `json:"name"`
	Confidence          float64  `json:"confidence"`
	Context             []string `json:"context,omitempty"`
	NeedsDisambiguation bool     `json:"needs_disambiguation"`
	DisambiguatedBy     string   `json:"disambiguated_by,omitempty"`
}

type registryData struct {
	Version        int                   `json:"version"`
	Mode           string                `json:"mode"`
	People         map[string]PersonInfo `json:"people"`
	Projects       []string              `json:"projects"`
	AmbiguousFlags []string              `json:"ambiguous_flags"`
	WikiCache      map[string]WikiResult `json:"wiki_cache"`
}

// Registry is a persistent entity registry backed by JSON.
type Registry struct {
	data    registryData
	path    string
	BaseURL string // Wikipedia API base URL, overridable for testing.
}

// Person is an input to Seed.
type Person struct {
	Name         string
	Relationship string
	Context      string
}

// LoadRegistry loads or creates the entity registry.
func LoadRegistry(configDir string) (*Registry, error) {
	if configDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		configDir = filepath.Join(home, ".mempalace")
	}
	path := filepath.Join(configDir, "entity_registry.json")

	r := &Registry{
		path:    path,
		BaseURL: "https://en.wikipedia.org/api/rest_v1/page/summary",
		data:    emptyData(),
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(data, &r.data); err != nil {
		return r, nil // corrupt file, start fresh
	}
	if r.data.People == nil {
		r.data.People = map[string]PersonInfo{}
	}
	if r.data.WikiCache == nil {
		r.data.WikiCache = map[string]WikiResult{}
	}
	return r, nil
}

func emptyData() registryData {
	return registryData{
		Version:        1,
		Mode:           "personal",
		People:         map[string]PersonInfo{},
		Projects:       []string{},
		AmbiguousFlags: []string{},
		WikiCache:      map[string]WikiResult{},
	}
}

// Save writes the registry to disk.
func (r *Registry) Save() error {
	if err := os.MkdirAll(filepath.Dir(r.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(r.data, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(r.path, b, 0o644)
}

// Seed initialises the registry from onboarding data.
func (r *Registry) Seed(mode string, people []Person, projects []string, aliases map[string]string) {
	r.data.Mode = mode
	r.data.Projects = projects

	reverseAliases := map[string]string{}
	for k, v := range aliases {
		reverseAliases[v] = k
	}

	for _, p := range people {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		ctx := p.Context
		if ctx == "" {
			ctx = "personal"
		}
		info := PersonInfo{
			Source:       "onboarding",
			Contexts:     []string{ctx},
			Relationship: p.Relationship,
			Confidence:   1.0,
		}
		if alias, ok := reverseAliases[name]; ok {
			info.Aliases = []string{alias}
		}
		r.data.People[name] = info

		// Register reverse alias too
		if alias, ok := reverseAliases[name]; ok {
			r.data.People[alias] = PersonInfo{
				Source:       "onboarding",
				Contexts:     []string{ctx},
				Aliases:      []string{name},
				Relationship: p.Relationship,
				Confidence:   1.0,
				Canonical:    name,
			}
		}
	}

	// Flag ambiguous names
	var ambiguous []string
	for name := range r.data.People {
		if commonEnglishWords[strings.ToLower(name)] {
			ambiguous = append(ambiguous, strings.ToLower(name))
		}
	}
	r.data.AmbiguousFlags = ambiguous
}

// Lookup looks up a word in the registry.
func (r *Registry) Lookup(word, context string) LookupResult {
	// 1. People
	for canonical, info := range r.data.People {
		match := strings.EqualFold(word, canonical)
		if !match {
			for _, a := range info.Aliases {
				if strings.EqualFold(word, a) {
					match = true
					break
				}
			}
		}
		if !match {
			continue
		}
		if context != "" && r.isAmbiguous(word) {
			resolved := r.disambiguate(word, context, info)
			if resolved != nil {
				return *resolved
			}
		}
		return LookupResult{
			Type:       "person",
			Confidence: info.Confidence,
			Source:     info.Source,
			Name:       canonical,
			Context:    info.Contexts,
		}
	}

	// 2. Projects
	for _, proj := range r.data.Projects {
		if strings.EqualFold(word, proj) {
			return LookupResult{
				Type:       "project",
				Confidence: 1.0,
				Source:     "onboarding",
				Name:       proj,
			}
		}
	}

	// 3. Wiki cache
	for cachedWord, cached := range r.data.WikiCache {
		if strings.EqualFold(word, cachedWord) && cached.Confirmed {
			return LookupResult{
				Type:       cached.InferredType,
				Confidence: cached.Confidence,
				Source:     "wiki",
				Name:       word,
			}
		}
	}

	return LookupResult{
		Type:       "unknown",
		Confidence: 0.0,
		Source:     "none",
		Name:       word,
	}
}

func (r *Registry) isAmbiguous(word string) bool {
	lower := strings.ToLower(word)
	for _, f := range r.data.AmbiguousFlags {
		if f == lower {
			return true
		}
	}
	return false
}

func (r *Registry) disambiguate(word, context string, info PersonInfo) *LookupResult {
	nameLower := strings.ToLower(word)
	ctxLower := strings.ToLower(context)
	escaped := regexp.QuoteMeta(nameLower)

	personScore := 0
	for _, tmpl := range personContextTemplates {
		pat := fmt.Sprintf(tmpl, escaped)
		if rx, err := regexp.Compile("(?i)" + pat); err == nil && rx.MatchString(ctxLower) {
			personScore++
		}
	}

	conceptScore := 0
	for _, tmpl := range conceptContextTemplates {
		pat := fmt.Sprintf(tmpl, escaped)
		if rx, err := regexp.Compile("(?i)" + pat); err == nil && rx.MatchString(ctxLower) {
			conceptScore++
		}
	}

	if personScore > conceptScore {
		conf := 0.7 + float64(personScore)*0.1
		if conf > 0.95 {
			conf = 0.95
		}
		return &LookupResult{
			Type:            "person",
			Confidence:      conf,
			Source:          info.Source,
			Name:            word,
			Context:         info.Contexts,
			DisambiguatedBy: "context_patterns",
		}
	}
	if conceptScore > personScore {
		conf := 0.7 + float64(conceptScore)*0.1
		if conf > 0.90 {
			conf = 0.90
		}
		return &LookupResult{
			Type:            "concept",
			Confidence:      conf,
			Source:          "context_disambiguated",
			Name:            word,
			DisambiguatedBy: "context_patterns",
		}
	}

	return nil // truly ambiguous
}

// Research looks up a word via Wikipedia. Caches the result.
func (r *Registry) Research(word string, autoConfirm bool) (WikiResult, error) {
	if cached, ok := r.data.WikiCache[word]; ok {
		return cached, nil
	}
	result := r.wikipediaLookup(word)
	result.Word = word
	result.Confirmed = autoConfirm

	r.data.WikiCache[word] = result
	if err := r.Save(); err != nil {
		return result, err
	}
	return result, nil
}

func (r *Registry) wikipediaLookup(word string) WikiResult {
	baseURL := r.BaseURL
	if baseURL == "" {
		baseURL = "https://en.wikipedia.org/api/rest_v1/page/summary"
	}
	reqURL := baseURL + "/" + url.PathEscape(word)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return WikiResult{InferredType: "unknown", Confidence: 0.0}
	}
	req.Header.Set("User-Agent", "MemPalace/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return WikiResult{InferredType: "unknown", Confidence: 0.0}
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 {
		return WikiResult{
			InferredType: "person",
			Confidence:   0.70,
			Note:         "not found in Wikipedia — likely a proper noun or unusual name",
		}
	}
	if resp.StatusCode != 200 {
		return WikiResult{InferredType: "unknown", Confidence: 0.0}
	}

	var data struct {
		Type        string `json:"type"`
		Extract     string `json:"extract"`
		Title       string `json:"title"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return WikiResult{InferredType: "unknown", Confidence: 0.0}
	}

	extract := strings.ToLower(data.Extract)
	title := data.Title
	if title == "" {
		title = word
	}

	summary := data.Extract
	if len(summary) > 200 {
		summary = summary[:200]
	}

	// Disambiguation page
	if data.Type == "disambiguation" {
		desc := strings.ToLower(data.Description)
		if strings.Contains(desc, "name") || strings.Contains(desc, "given name") {
			return WikiResult{
				InferredType: "person",
				Confidence:   0.65,
				WikiSummary:  summary,
				WikiTitle:    title,
				Note:         "disambiguation page with name entries",
			}
		}
		return WikiResult{
			InferredType: "ambiguous",
			Confidence:   0.4,
			WikiSummary:  summary,
			WikiTitle:    title,
		}
	}

	// Name indicators
	for _, phrase := range nameIndicatorPhrases {
		if strings.Contains(extract, phrase) {
			conf := 0.80
			wordLower := strings.ToLower(word)
			if strings.Contains(extract, wordLower+" is a") || strings.Contains(extract, wordLower+" (name") {
				conf = 0.90
			}
			return WikiResult{
				InferredType: "person",
				Confidence:   conf,
				WikiSummary:  summary,
				WikiTitle:    title,
			}
		}
	}

	// Place indicators
	for _, phrase := range placeIndicatorPhrases {
		if strings.Contains(extract, phrase) {
			return WikiResult{
				InferredType: "place",
				Confidence:   0.80,
				WikiSummary:  summary,
				WikiTitle:    title,
			}
		}
	}

	return WikiResult{
		InferredType: "concept",
		Confidence:   0.60,
		WikiSummary:  summary,
		WikiTitle:    title,
	}
}

// LearnFromText scans text for new entity candidates and adds high-confidence people.
func (r *Registry) LearnFromText(text string, minConfidence float64) ([]Entity, error) {
	lines := strings.Split(text, "\n")
	candidates := ExtractCandidates(text)
	var newCandidates []Entity

	for name, frequency := range candidates {
		if _, ok := r.data.People[name]; ok {
			continue
		}
		isProject := false
		for _, proj := range r.data.Projects {
			if strings.EqualFold(name, proj) {
				isProject = true
				break
			}
		}
		if isProject {
			continue
		}

		scores := ScoreEntity(name, text, lines)
		entity := ClassifyEntity(name, frequency, scores)

		if entity.Type == "person" && entity.Confidence >= minConfidence {
			ctx := r.data.Mode
			if ctx == "combo" {
				ctx = "personal"
			}
			r.data.People[name] = PersonInfo{
				Source:     "learned",
				Contexts:   []string{ctx},
				Confidence: entity.Confidence,
				SeenCount:  frequency,
			}
			if commonEnglishWords[strings.ToLower(name)] {
				found := false
				for _, f := range r.data.AmbiguousFlags {
					if f == strings.ToLower(name) {
						found = true
						break
					}
				}
				if !found {
					r.data.AmbiguousFlags = append(r.data.AmbiguousFlags, strings.ToLower(name))
				}
			}
			newCandidates = append(newCandidates, entity)
		}
	}

	if len(newCandidates) > 0 {
		if err := r.Save(); err != nil {
			return newCandidates, err
		}
	}
	return newCandidates, nil
}

// ExtractPeopleFromQuery finds known person names in a query string.
func (r *Registry) ExtractPeopleFromQuery(query string) []string {
	var found []string
	for canonical, info := range r.data.People {
		namesToCheck := append([]string{canonical}, info.Aliases...)
		for _, name := range namesToCheck {
			escaped := regexp.QuoteMeta(name)
			rx, err := regexp.Compile(`(?i)\b` + escaped + `\b`)
			if err != nil {
				continue
			}
			if rx.MatchString(query) {
				if r.isAmbiguous(name) {
					result := r.disambiguate(name, query, info)
					if result != nil && result.Type == "person" {
						if !containsStr(found, canonical) {
							found = append(found, canonical)
						}
					}
				} else if !containsStr(found, canonical) {
					found = append(found, canonical)
				}
			}
		}
	}
	return found
}

// Summary returns a human-readable summary of the registry.
func (r *Registry) Summary() string {
	peopleNames := make([]string, 0, len(r.data.People))
	for name := range r.data.People {
		peopleNames = append(peopleNames, name)
	}
	if len(peopleNames) > 8 {
		peopleNames = append(peopleNames[:8], "...")
	}
	projects := strings.Join(r.data.Projects, ", ")
	if projects == "" {
		projects = "(none)"
	}
	ambiguous := strings.Join(r.data.AmbiguousFlags, ", ")
	if ambiguous == "" {
		ambiguous = "(none)"
	}
	return fmt.Sprintf("Mode: %s\nPeople: %d (%s)\nProjects: %s\nAmbiguous flags: %s\nWiki cache: %d entries",
		r.data.Mode, len(r.data.People), strings.Join(peopleNames, ", "),
		projects, ambiguous, len(r.data.WikiCache))
}

func containsStr(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}
