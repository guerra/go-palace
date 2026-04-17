package entity_test

import (
	"strings"
	"testing"

	"github.com/guerra/go-palace/pkg/entity"
)

// hasType reports whether at least one Entity of t is present.
func hasType(es []entity.Entity, t entity.EntityType) bool {
	for _, e := range es {
		if e.Type == t {
			return true
		}
	}
	return false
}

// findByName returns the first Entity with matching Name, or zero + false.
func findByName(es []entity.Entity, name string) (entity.Entity, bool) {
	for _, e := range es {
		if e.Name == name {
			return e, true
		}
	}
	return entity.Entity{}, false
}

// countType counts entities of type t.
func countType(es []entity.Entity, t entity.EntityType) int {
	n := 0
	for _, e := range es {
		if e.Type == t {
			n++
		}
	}
	return n
}

func TestDetectPerson(t *testing.T) {
	// Three Riley occurrences with dialogue + action verbs + direct address
	// provide 2+ signal categories and ps >= 5.
	content := "Riley said she was excited.\n" +
		"Riley laughed at the joke.\n" +
		"Hey Riley, thanks for the help!"
	got := entity.Detect(content)
	e, ok := findByName(got, "Riley")
	if !ok {
		t.Fatalf("Riley not detected; got=%+v", got)
	}
	if e.Type != entity.TypePerson {
		t.Errorf("Riley type: got %q want person", e.Type)
	}
	if e.Confidence < 0.5 {
		t.Errorf("Riley confidence: got %f want >= 0.5", e.Confidence)
	}
}

func TestDetectProject(t *testing.T) {
	content := "We are building ChromaDB. Install ChromaDB with pip. " +
		"ChromaDB.py holds the source. The ChromaDB pipeline is clean."
	got := entity.Detect(content)
	found := false
	for _, e := range got {
		if e.Name == "ChromaDB" && e.Type == entity.TypeProject {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("ChromaDB not detected as project; got=%+v", got)
	}
}

func TestDetectPlace(t *testing.T) {
	content := "Hiked Mount Washington last weekend. The Hudson River was beautiful."
	got := entity.Detect(content)
	if !hasType(got, entity.TypePlace) {
		t.Fatalf("no place detected; got=%+v", got)
	}
	// At least one of these two should be recognized.
	found := false
	for _, e := range got {
		if e.Type == entity.TypePlace &&
			(strings.Contains(e.Name, "Washington") || strings.Contains(e.Name, "Hudson")) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected Washington or Hudson place; got=%+v", got)
	}
}

func TestDetectTool(t *testing.T) {
	content := "I use docker and git daily, plus python for scripting."
	got := entity.Detect(content)
	names := map[string]bool{}
	for _, e := range got {
		if e.Type == entity.TypeTool {
			names[strings.ToLower(e.Name)] = true
		}
	}
	for _, want := range []string{"docker", "git", "python"} {
		if !names[want] {
			t.Errorf("expected tool %q in detected names %v", want, names)
		}
	}
}

func TestDetectDate(t *testing.T) {
	content := "Meeting on 2026-04-17. Previously March 15, 2026 was planned."
	got := entity.Detect(content)
	dates := []string{}
	for _, e := range got {
		if e.Type == entity.TypeDate {
			dates = append(dates, e.Name)
		}
	}
	if len(dates) < 2 {
		t.Fatalf("expected >= 2 dates, got %v", dates)
	}
	wantISO := false
	wantNatural := false
	for _, d := range dates {
		if d == "2026-04-17" {
			wantISO = true
		}
		if strings.Contains(d, "March") {
			wantNatural = true
		}
	}
	if !wantISO {
		t.Errorf("ISO date 2026-04-17 not found in %v", dates)
	}
	if !wantNatural {
		t.Errorf("natural date 'March 15, 2026' not found in %v", dates)
	}
}

func TestDetectURL(t *testing.T) {
	content := "Visit https://example.com/docs for the manual."
	got := entity.Detect(content)
	e, ok := findByName(got, "https://example.com/docs")
	if !ok {
		t.Fatalf("URL not detected; got=%+v", got)
	}
	if e.Type != entity.TypeURL {
		t.Errorf("type: got %q want url", e.Type)
	}
}

func TestDetectEmail(t *testing.T) {
	content := "Contact admin@example.com for access."
	got := entity.Detect(content)
	e, ok := findByName(got, "admin@example.com")
	if !ok {
		t.Fatalf("email not detected; got=%+v", got)
	}
	if e.Type != entity.TypeEmail {
		t.Errorf("type: got %q want email", e.Type)
	}
	if e.Canonical != "admin@example.com" {
		t.Errorf("canonical: got %q", e.Canonical)
	}
}

// TestDetectMixedContent is the gp-3 acceptance criterion: a single
// paragraph with date + person + email + tool + url yields distinct entities.
func TestDetectMixedContent(t *testing.T) {
	content := "On 2026-04-17, Riley met with alice@example.com to discuss " +
		"docker migration. See https://docs.example.com for details."
	got := entity.Detect(content)
	// At least 4 distinct types present.
	types := map[entity.EntityType]bool{}
	for _, e := range got {
		types[e.Type] = true
	}
	wantTypes := []entity.EntityType{
		entity.TypeDate, entity.TypeEmail, entity.TypeTool, entity.TypeURL,
	}
	for _, wt := range wantTypes {
		if !types[wt] {
			t.Errorf("missing type %q; got types=%v entities=%+v", wt, types, got)
		}
	}
}

func TestDetectOffsetsAreByteOffsets(t *testing.T) {
	content := "café: https://cafe.test/"
	got := entity.Detect(content)
	if len(got) == 0 {
		t.Fatalf("no entities detected")
	}
	for _, e := range got {
		end := e.Offset + len(e.Name)
		if e.Offset < 0 || end > len(content) {
			t.Errorf("offset out of range: name=%q offset=%d len=%d contentLen=%d",
				e.Name, e.Offset, len(e.Name), len(content))
			continue
		}
		if content[e.Offset:end] != e.Name {
			t.Errorf("byte-offset mismatch: name=%q offset=%d got=%q",
				e.Name, e.Offset, content[e.Offset:end])
		}
	}
}

func TestDetectEmpty(t *testing.T) {
	if got := entity.Detect(""); got != nil {
		t.Errorf("Detect(\"\") = %v, want nil", got)
	}
}

func TestDetectNoMatches(t *testing.T) {
	got := entity.Detect("nothing interesting happens in this sentence")
	if len(got) != 0 {
		t.Errorf("expected 0 entities, got %+v", got)
	}
}

func TestDetectSortedByOffset(t *testing.T) {
	content := "At 2026-01-01 the git install happened at https://a.b/ later."
	got := entity.Detect(content)
	if len(got) < 2 {
		t.Fatalf("need >= 2 entities to test sort, got %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].Offset < got[i-1].Offset {
			t.Errorf("entities not sorted by Offset: [%d]=%d < [%d]=%d",
				i, got[i].Offset, i-1, got[i-1].Offset)
		}
	}
}

func TestDetectNoDuplicates(t *testing.T) {
	// Three distinct "docker" occurrences at distinct offsets => 3 entities.
	content := "docker and docker and docker."
	got := entity.Detect(content)
	if n := countType(got, entity.TypeTool); n != 3 {
		t.Errorf("expected 3 tool entities (distinct offsets), got %d; entities=%+v", n, got)
	}
}

func TestDetectStopwordsFiltered(t *testing.T) {
	// Pure stopwords must not yield person/project entities.
	content := "The The The Something Something Something"
	got := entity.Detect(content)
	for _, e := range got {
		if e.Type == entity.TypePerson || e.Type == entity.TypeProject {
			t.Errorf("stopword leaked as %q: %+v", e.Type, e)
		}
	}
}

func TestDetectURLCanonicalization(t *testing.T) {
	content := "See HTTPS://Example.COM/Foo now."
	got := entity.Detect(content)
	e, ok := findByName(got, "HTTPS://Example.COM/Foo")
	if !ok {
		t.Fatalf("URL not detected in %q; got=%+v", content, got)
	}
	if !strings.HasPrefix(e.Canonical, "https://example.com") {
		t.Errorf("canonical not lowercased: %q", e.Canonical)
	}
}

func TestDetectEmailCanonicalization(t *testing.T) {
	content := "write to Bob.Loblaw@Example.COM please"
	got := entity.Detect(content)
	e, ok := findByName(got, "Bob.Loblaw@Example.COM")
	if !ok {
		t.Fatalf("email not detected; got=%+v", got)
	}
	if e.Canonical != "bob.loblaw@example.com" {
		t.Errorf("canonical: got %q want bob.loblaw@example.com", e.Canonical)
	}
}

func TestDetectDateCanonicalization(t *testing.T) {
	got := entity.Detect("deadline 2026-04-17 is firm")
	e, ok := findByName(got, "2026-04-17")
	if !ok {
		t.Fatalf("ISO date not detected; got=%+v", got)
	}
	if e.Canonical != "2026-04-17" {
		t.Errorf("canonical: got %q want 2026-04-17", e.Canonical)
	}
}

func TestDetectTypesHaveExpectedSet(t *testing.T) {
	// Sanity: AllTypes contains the named constants and no duplicates.
	seen := map[entity.EntityType]bool{}
	for _, tp := range entity.AllTypes {
		if seen[tp] {
			t.Errorf("duplicate type in AllTypes: %q", tp)
		}
		seen[tp] = true
	}
	for _, want := range []entity.EntityType{
		entity.TypePerson, entity.TypePlace, entity.TypeProject,
		entity.TypeTool, entity.TypeDate, entity.TypeURL,
		entity.TypeEmail, entity.TypeUncertain,
	} {
		if !seen[want] {
			t.Errorf("AllTypes missing %q", want)
		}
	}
}

func BenchmarkDetect10KB(b *testing.B) {
	// Assemble a ~10KB realistic payload.
	chunk := "Riley met alice@example.com on 2026-04-17 to discuss docker. " +
		"See https://docs.example.com for the ChromaDB build notes. "
	var sb strings.Builder
	for sb.Len() < 10*1024 {
		sb.WriteString(chunk)
	}
	content := sb.String()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = entity.Detect(content)
	}
}
