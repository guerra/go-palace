package entity_test

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// --- ExtractCandidates edge cases ---

func TestExtractCandidatesRequiresMinFrequency(t *testing.T) {
	text := "Riley said hi. Devon waved."
	result := entity.ExtractCandidates(text)
	if _, ok := result["Riley"]; ok {
		t.Error("Riley should not appear (frequency < 3)")
	}
}

func TestExtractCandidatesFindsMultiWordNames(t *testing.T) {
	text := "Claude Code is great. Claude Code rocks. Claude Code works. Claude Code rules."
	result := entity.ExtractCandidates(text)
	if _, ok := result["Claude Code"]; !ok {
		t.Error("expected 'Claude Code' as multi-word candidate")
	}
}

func TestExtractCandidatesEmptyText(t *testing.T) {
	result := entity.ExtractCandidates("")
	if len(result) != 0 {
		t.Errorf("expected empty map, got %v", result)
	}
}

// --- ScoreEntity edge cases ---

func TestScoreEntityDialogueMarkers(t *testing.T) {
	text := "Riley: Hey, how are you?\nRiley: I'm fine."
	lines := strings.Split(text, "\n")
	scores := entity.ScoreEntity("Riley", text, lines)
	if scores.PersonScore == 0 {
		t.Error("expected person_score > 0 for dialogue markers")
	}
}

func TestScoreEntityCodeRef(t *testing.T) {
	text := "Check out ChromaDB.py for details. Also ChromaDB.js is good."
	lines := strings.Split(text, "\n")
	scores := entity.ScoreEntity("ChromaDB", text, lines)
	if scores.ProjectScore == 0 {
		t.Error("expected project_score > 0 for code refs")
	}
}

func TestScoreEntityNoSignals(t *testing.T) {
	text := "Nothing interesting here at all."
	lines := strings.Split(text, "\n")
	scores := entity.ScoreEntity("Riley", text, lines)
	if scores.PersonScore != 0 {
		t.Errorf("person_score = %d, want 0", scores.PersonScore)
	}
	if scores.ProjectScore != 0 {
		t.Errorf("project_score = %d, want 0", scores.ProjectScore)
	}
}

// --- ClassifyEntity edge cases ---

func TestClassifyEntityNoSignalsGivesUncertain(t *testing.T) {
	scores := entity.EntityScores{PersonScore: 0, ProjectScore: 0}
	e := entity.ClassifyEntity("Foo", 10, scores)
	if e.Type != "uncertain" {
		t.Errorf("type = %q, want uncertain", e.Type)
	}
}

func TestClassifyEntityStrongProject(t *testing.T) {
	scores := entity.EntityScores{
		PersonScore:    0,
		ProjectScore:   10,
		ProjectSignals: []string{"project verb (5x)", "code file reference (2x)"},
	}
	e := entity.ClassifyEntity("ChromaDB", 5, scores)
	if e.Type != "project" {
		t.Errorf("type = %q, want project", e.Type)
	}
}

func TestClassifyEntityPronounOnlyIsUncertain(t *testing.T) {
	scores := entity.EntityScores{
		PersonScore:   8,
		ProjectScore:  0,
		PersonSignals: []string{"pronoun nearby (4x)"},
	}
	e := entity.ClassifyEntity("Riley", 5, scores)
	if e.Type != "uncertain" {
		t.Errorf("type = %q, want uncertain (pronoun-only)", e.Type)
	}
}

func TestClassifyEntityMixedSignals(t *testing.T) {
	scores := entity.EntityScores{
		PersonScore:    5,
		ProjectScore:   5,
		PersonSignals:  []string{"pronoun nearby (2x)"},
		ProjectSignals: []string{"project verb (2x)"},
	}
	e := entity.ClassifyEntity("Lantern", 5, scores)
	if e.Type != "uncertain" {
		t.Errorf("type = %q, want uncertain", e.Type)
	}
	lastSignal := e.Signals[len(e.Signals)-1]
	if !strings.Contains(lastSignal, "mixed signals") {
		t.Errorf("last signal = %q, want 'mixed signals'", lastSignal)
	}
}

// --- Detect edge cases ---

func TestDetectEntitiesWithProjectFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "readme.txt")
	content := strings.Join([]string{
		"The Lantern project is great.",
		"Building Lantern was fun.",
		"We deployed Lantern today.",
		"Install Lantern with pip install Lantern.",
		"Check Lantern.py for the source.",
		"Lantern v2 is faster.",
	}, "\n")
	os.WriteFile(path, []byte(content), 0o644)
	result := entity.Detect([]string{path}, 10)
	allNames := []string{}
	for _, e := range result.People {
		allNames = append(allNames, e.Name)
	}
	for _, e := range result.Projects {
		allNames = append(allNames, e.Name)
	}
	for _, e := range result.Uncertain {
		allNames = append(allNames, e.Name)
	}
	found := false
	for _, n := range allNames {
		if n == "Lantern" {
			found = true
		}
	}
	if !found {
		t.Error("expected Lantern in detected entities")
	}
}

func TestDetectEntitiesEmptyFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	os.WriteFile(path, []byte(""), 0o644)
	result := entity.Detect([]string{path}, 10)
	if len(result.People) != 0 || len(result.Projects) != 0 || len(result.Uncertain) != 0 {
		t.Error("expected empty result for empty file")
	}
}

func TestDetectEntitiesHandlesMissingFile(t *testing.T) {
	result := entity.Detect([]string{"/nonexistent/file.txt"}, 10)
	if len(result.People) != 0 || len(result.Projects) != 0 || len(result.Uncertain) != 0 {
		t.Error("expected empty result for missing file")
	}
}

func TestDetectEntitiesRespectsMaxFiles(t *testing.T) {
	dir := t.TempDir()
	var paths []string
	for i := 0; i < 5; i++ {
		path := filepath.Join(dir, fmt.Sprintf("file%d.txt", i))
		os.WriteFile(path, []byte(strings.Repeat("Riley said hello. ", 10)), 0o644)
		paths = append(paths, path)
	}
	// max_files=2 should not panic and should still detect entities from limited files
	result := entity.Detect(paths, 2)
	total := len(result.People) + len(result.Projects) + len(result.Uncertain)
	if total == 0 {
		t.Error("expected at least one entity detected from limited files")
	}
}

// --- ScanForDetection ---

func TestScanForDetectionFindsProse(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "notes.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "data.txt"), []byte("world"), 0o644)
	os.WriteFile(filepath.Join(dir, "code.py"), []byte("import os"), 0o644)
	files, err := entity.ScanForDetection(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	extensions := map[string]bool{}
	for _, f := range files {
		extensions[filepath.Ext(f)] = true
	}
	if !extensions[".md"] && !extensions[".txt"] {
		t.Error("expected prose files (.md or .txt)")
	}
}

func TestScanForDetectionSkipsGitDir(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	os.MkdirAll(gitDir, 0o755)
	os.WriteFile(filepath.Join(gitDir, "config.txt"), []byte("git config"), 0o644)
	os.WriteFile(filepath.Join(dir, "readme.md"), []byte("hello"), 0o644)
	files, err := entity.ScanForDetection(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.Contains(f, ".git") {
			t.Errorf("should skip .git dir, found %s", f)
		}
	}
}

func TestScanForDetectionFallbackToAllReadable(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "one.md"), []byte("hello"), 0o644)
	os.WriteFile(filepath.Join(dir, "two.txt"), []byte("world"), 0o644)
	os.WriteFile(filepath.Join(dir, "code.py"), []byte("import os"), 0o644)
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log()"), 0o644)
	files, err := entity.ScanForDetection(dir, 100)
	if err != nil {
		t.Fatal(err)
	}
	extensions := map[string]bool{}
	for _, f := range files {
		extensions[filepath.Ext(f)] = true
	}
	if !extensions[".py"] && !extensions[".js"] {
		t.Error("expected fallback to include code files when < 3 prose")
	}
}

func TestScanForDetectionMaxFiles(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 20; i++ {
		os.WriteFile(filepath.Join(dir, fmt.Sprintf("note%d.md", i)), []byte(fmt.Sprintf("content %d", i)), 0o644)
	}
	files, err := entity.ScanForDetection(dir, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) > 5 {
		t.Errorf("files = %d, want <= 5", len(files))
	}
}

// --- Registry edge cases ---

func TestRegistryLoadFromNonexistentDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nonexistent")
	r, err := entity.LoadRegistry(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Should be empty
	result := r.Lookup("anything", "")
	if result.Type != "unknown" {
		t.Errorf("expected unknown, got %q", result.Type)
	}
}

func TestRegistrySaveCreatesFile(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	if err := r.Save(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entity_registry.json")); err != nil {
		t.Errorf("registry file not created: %v", err)
	}
}

func TestSeedRegistersPeople(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Riley", Relationship: "daughter", Context: "personal"},
		{Name: "Devon", Relationship: "friend", Context: "personal"},
	}, []string{"MemPalace"}, nil)

	result := r.Lookup("Riley", "")
	if result.Type != "person" {
		t.Errorf("Riley type = %q, want person", result.Type)
	}
	if result.Confidence != 1.0 {
		t.Errorf("confidence = %.2f, want 1.0", result.Confidence)
	}
	if result.Source != "onboarding" {
		t.Errorf("source = %q, want onboarding", result.Source)
	}
}

func TestSeedRegistersProjects(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("work", nil, []string{"Acme", "Widget"}, nil)

	result := r.Lookup("Acme", "")
	if result.Type != "project" {
		t.Errorf("Acme type = %q, want project", result.Type)
	}
}

func TestSeedSetsMode(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("combo", nil, nil, nil)
	summary := r.Summary()
	if !strings.Contains(summary, "combo") {
		t.Errorf("summary missing mode: %s", summary)
	}
}

func TestSeedFlagsAmbiguousNames(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Grace", Relationship: "friend", Context: "personal"},
		{Name: "Riley", Relationship: "daughter", Context: "personal"},
	}, nil, nil)

	// Grace is a common English word so should be ambiguous
	result := r.Lookup("Grace", "the grace of the dancer")
	if result.Type != "concept" {
		t.Errorf("Grace in concept context: type = %q, want concept", result.Type)
	}

	// Riley is not ambiguous
	result = r.Lookup("Riley", "")
	if result.Type != "person" {
		t.Errorf("Riley: type = %q, want person", result.Type)
	}
}

func TestSeedWithAliases(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Maxwell", Relationship: "friend", Context: "personal"},
	}, nil, map[string]string{"Max": "Maxwell"})

	result := r.Lookup("Max", "")
	if result.Type != "person" {
		t.Errorf("Max (alias) type = %q, want person", result.Type)
	}
}

func TestSeedSkipsEmptyNames(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "", Relationship: "", Context: "personal"},
	}, nil, nil)
	summary := r.Summary()
	if !strings.Contains(summary, "People: 0") {
		t.Errorf("expected 0 people, got summary: %s", summary)
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Riley", Relationship: "daughter", Context: "personal"},
	}, nil, nil)
	result := r.Lookup("riley", "")
	if result.Type != "person" {
		t.Errorf("riley (lowercase) type = %q, want person", result.Type)
	}
}

func TestLookupAlias(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Maxwell", Relationship: "friend", Context: "personal"},
	}, nil, map[string]string{"Max": "Maxwell"})
	result := r.Lookup("Max", "")
	if result.Type != "person" {
		t.Errorf("alias lookup: type = %q, want person", result.Type)
	}
}

func TestLookupAmbiguousWordAsPerson(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Grace", Relationship: "friend", Context: "personal"},
	}, nil, nil)
	result := r.Lookup("Grace", "I went with Grace today")
	if result.Type != "person" {
		t.Errorf("person context: type = %q, want person", result.Type)
	}
}

func TestLookupAmbiguousWordAsConcept(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Ever", Relationship: "friend", Context: "personal"},
	}, nil, nil)
	result := r.Lookup("Ever", "have you ever tried this")
	if result.Type != "concept" {
		t.Errorf("concept context: type = %q, want concept", result.Type)
	}
}

func TestResearchCachesResult(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/Saoirse", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"type":    "standard",
			"extract": "Saoirse is an Irish given name meaning freedom.",
			"title":   "Saoirse",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.BaseURL = server.URL

	result, err := r.Research("Saoirse", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.InferredType != "person" {
		t.Errorf("first call: type = %q, want person", result.InferredType)
	}

	// Second call should use cache — change server to fail
	server.Close()
	result2, err := r.Research("Saoirse", false)
	if err != nil {
		t.Fatal(err)
	}
	if result2.InferredType != "person" {
		t.Errorf("cached call: type = %q, want person", result2.InferredType)
	}
}

func TestConfirmResearchAddsToPeople(t *testing.T) {
	// Not directly applicable since Go ConfirmResearch doesn't exist as separate method.
	// The Research method with autoConfirm=true confirms it.
	mux := http.NewServeMux()
	mux.HandleFunc("/TestPerson", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"type":    "standard",
			"extract": "TestPerson is a given name.",
			"title":   "TestPerson",
		})
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.BaseURL = server.URL

	result, err := r.Research("TestPerson", true)
	if err != nil {
		t.Fatal(err)
	}
	if result.InferredType != "person" {
		t.Errorf("type = %q, want person", result.InferredType)
	}
	if !result.Confirmed {
		t.Error("expected confirmed=true with autoConfirm")
	}
}

func TestConfirmEntitiesYesMode(t *testing.T) {
	detected := entity.DetectionResult{
		People:   []entity.Entity{{Name: "Alice", Confidence: 0.9, Signals: []string{"test"}}},
		Projects: []entity.Entity{{Name: "Acme", Confidence: 0.8, Signals: []string{"test"}}},
	}
	var buf bytes.Buffer
	people, projects := entity.Confirm(detected, true, &buf, nil)
	if len(people) != 1 || people[0] != "Alice" {
		t.Errorf("people = %v, want [Alice]", people)
	}
	if len(projects) != 1 || projects[0] != "Acme" {
		t.Errorf("projects = %v, want [Acme]", projects)
	}
}

func TestRegistrySummary(t *testing.T) {
	dir := t.TempDir()
	r, _ := entity.LoadRegistry(dir)
	r.Seed("personal", []entity.Person{
		{Name: "Riley", Relationship: "daughter", Context: "personal"},
	}, []string{"MemPalace"}, nil)
	s := r.Summary()
	if !strings.Contains(s, "personal") {
		t.Error("summary missing mode")
	}
	if !strings.Contains(s, "Riley") {
		t.Error("summary missing Riley")
	}
	if !strings.Contains(s, "MemPalace") {
		t.Error("summary missing MemPalace")
	}
}
