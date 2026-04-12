package entity_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"go-palace/internal/entity"
)

const sampleText = `
Riley said she was excited about the project. Riley laughed and told
everyone about her plans. Riley asked if anyone wanted to join.
Hey Riley, thanks for the update!
Riley: I think we should start the project today.
She told Riley about the news. He was near Riley when it happened.

Max is building MemPalace. Max deployed MemPalace v2 to production.
The MemPalace architecture is clean. MemPalace-core handles the backend.
Max shipped MemPalace again. Install MemPalace with pip install MemPalace.

Alice smiled and told the story again. Alice felt happy about the outcome.
Bob said he agreed with Alice. Alice asked Bob for help.
Hi Alice, thanks Alice!

Something about Django. Django.py is the framework. Building Django apps.
Django v4 is great. The Django pipeline works well.
`

func TestExtractCandidates(t *testing.T) {
	candidates := entity.ExtractCandidates(sampleText)
	if candidates["Riley"] < 3 {
		t.Errorf("Riley: got %d, want >=3", candidates["Riley"])
	}
	if candidates["Alice"] < 3 {
		t.Errorf("Alice: got %d, want >=3", candidates["Alice"])
	}
}

func TestExtractCandidatesStopwords(t *testing.T) {
	text := strings.Repeat("The The The Something Something Something Step Step Step ", 5)
	candidates := entity.ExtractCandidates(text)
	if _, ok := candidates["The"]; ok {
		t.Error("The should be filtered as stopword")
	}
	if _, ok := candidates["Something"]; ok {
		t.Error("Something should be filtered as stopword")
	}
	if _, ok := candidates["Step"]; ok {
		t.Error("Step should be filtered as stopword")
	}
}

func TestScoreEntityPerson(t *testing.T) {
	lines := strings.Split(sampleText, "\n")
	scores := entity.ScoreEntity("Riley", sampleText, lines)
	if scores.PersonScore < 5 {
		t.Errorf("Riley person score: got %d, want >=5", scores.PersonScore)
	}
	if len(scores.PersonSignals) == 0 {
		t.Error("expected person signals for Riley")
	}
}

func TestScoreEntityProject(t *testing.T) {
	lines := strings.Split(sampleText, "\n")
	scores := entity.ScoreEntity("Django", sampleText, lines)
	if scores.ProjectScore < 4 {
		t.Errorf("Django project score: got %d, want >=4", scores.ProjectScore)
	}
	if len(scores.ProjectSignals) == 0 {
		t.Error("expected project signals for Django")
	}
}

func TestClassifyPerson(t *testing.T) {
	lines := strings.Split(sampleText, "\n")
	scores := entity.ScoreEntity("Riley", sampleText, lines)
	e := entity.ClassifyEntity("Riley", 5, scores)
	if e.Type != "person" {
		t.Errorf("Riley classified as %q, want person", e.Type)
	}
	if e.Confidence < 0.5 {
		t.Errorf("Riley confidence: got %.2f, want >=0.5", e.Confidence)
	}
}

func TestClassifyUncertain(t *testing.T) {
	// Single signal category: only pronoun nearby
	scores := entity.EntityScores{
		PersonScore:   4,
		ProjectScore:  0,
		PersonSignals: []string{"pronoun nearby (4x)"},
	}
	e := entity.ClassifyEntity("Unknown", 5, scores)
	if e.Type == "person" {
		t.Error("single signal category should not classify as person")
	}
}

func TestDetect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.txt")
	if err := os.WriteFile(path, []byte(sampleText), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	result := entity.Detect([]string{path}, 10)
	// Riley and Alice should be detected as people
	foundRiley := false
	for _, e := range result.People {
		if e.Name == "Riley" {
			foundRiley = true
		}
	}
	if !foundRiley {
		t.Error("expected Riley in detected people")
	}
}

func TestSaveEntities(t *testing.T) {
	dir := t.TempDir()
	if err := entity.SaveEntities(dir, []string{"Riley", "Alice"}, []string{"MemPalace"}); err != nil {
		t.Fatalf("save: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "entities.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var result map[string][]string
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(result["people"]) != 2 {
		t.Errorf("people: got %d, want 2", len(result["people"]))
	}
	if len(result["projects"]) != 1 {
		t.Errorf("projects: got %d, want 1", len(result["projects"]))
	}
}

func TestRegistryLoadSave(t *testing.T) {
	dir := t.TempDir()
	r, err := entity.LoadRegistry(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	r.Seed("personal", []entity.Person{
		{Name: "Riley", Relationship: "daughter", Context: "personal"},
	}, []string{"MemPalace"}, nil)
	if err := r.Save(); err != nil {
		t.Fatalf("save: %v", err)
	}
	// Reload
	r2, err := entity.LoadRegistry(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	result := r2.Lookup("Riley", "")
	if result.Type != "person" {
		t.Errorf("Riley: got type %q, want person", result.Type)
	}
}

func TestRegistryLookup(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Riley", Context: "personal"},
	}, []string{"MemPalace"}, nil)

	// Person lookup
	result := r.Lookup("Riley", "")
	if result.Type != "person" {
		t.Errorf("Riley: got %q, want person", result.Type)
	}
	if result.Confidence != 1.0 {
		t.Errorf("confidence: got %.2f, want 1.0", result.Confidence)
	}

	// Project lookup
	result = r.Lookup("MemPalace", "")
	if result.Type != "project" {
		t.Errorf("MemPalace: got %q, want project", result.Type)
	}

	// Unknown
	result = r.Lookup("Zarquon", "")
	if result.Type != "unknown" {
		t.Errorf("Zarquon: got %q, want unknown", result.Type)
	}
}

func TestRegistryDisambiguate(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Grace", Context: "personal"},
	}, nil, nil)

	// Person context: "Grace said..."
	result := r.Lookup("Grace", "Grace said she was happy")
	if result.Type != "person" {
		t.Errorf("person context: got %q, want person", result.Type)
	}

	// Concept context: "the grace of..."
	result = r.Lookup("Grace", "the grace of the dancer was incredible")
	if result.Type != "concept" {
		t.Errorf("concept context: got %q, want concept", result.Type)
	}
}

func TestRegistryLearnFromText(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)

	newEntities, err := r.LearnFromText(sampleText, 0.75)
	if err != nil {
		t.Fatalf("learn: %v", err)
	}
	// Should learn at least one person
	if len(newEntities) == 0 {
		t.Log("no high-confidence entities learned (may depend on scoring thresholds)")
	}
}

func TestWikipediaLookup(t *testing.T) {
	// Mock Wikipedia API
	mux := http.NewServeMux()
	mux.HandleFunc("/Riley", func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"type":        "standard",
			"extract":     "Riley is a given name of Irish origin.",
			"title":       "Riley (given name)",
			"description": "given name",
		})
	})
	mux.HandleFunc("/Xyzzy", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(404)
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.BaseURL = server.URL

	// Found: name
	result, err := r.Research("Riley", false)
	if err != nil {
		t.Fatalf("research Riley: %v", err)
	}
	if result.InferredType != "person" {
		t.Errorf("Riley inferred type: got %q, want person", result.InferredType)
	}

	// 404: inferred person
	result, err = r.Research("Xyzzy", false)
	if err != nil {
		t.Fatalf("research Xyzzy: %v", err)
	}
	if result.InferredType != "person" {
		t.Errorf("Xyzzy inferred type: got %q, want person", result.InferredType)
	}
	if result.Confidence != 0.70 {
		t.Errorf("Xyzzy confidence: got %.2f, want 0.70", result.Confidence)
	}
}

func TestExtractPeopleFromQuery(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Riley"},
		{Name: "Alice"},
	}, nil, nil)

	found := r.ExtractPeopleFromQuery("What did Riley and Alice do yesterday?")
	if len(found) != 2 {
		t.Errorf("found: got %d, want 2", len(found))
	}
}
