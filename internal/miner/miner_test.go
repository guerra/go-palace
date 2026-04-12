package miner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go-palace/internal/room"
	"go-palace/pkg/embed"
	"go-palace/pkg/palace"
)

// ---- chunker ---------------------------------------------------------------

func TestChunkerBasicEmpty(t *testing.T) {
	if got := ChunkText("   "); len(got) != 0 {
		t.Errorf("empty input → %+v, want nil", got)
	}
}

func TestChunkerShortSkipsBelowMin(t *testing.T) {
	if got := ChunkText("short"); len(got) != 0 {
		t.Errorf("short input → %+v, want nil (len < MinChunkSize)", got)
	}
}

func TestChunkerBoundariesParagraph(t *testing.T) {
	// Build a 1500-char string with a double-newline at position 900.
	// First chunk should end at 900 (paragraph boundary > ChunkSize/2),
	// second starts at 900 - ChunkOverlap = 800.
	left := strings.Repeat("a", 900)
	right := strings.Repeat("b", 1500-902)
	content := left + "\n\n" + right
	if len(content) != 1500 {
		t.Fatalf("setup: len=%d", len(content))
	}
	chunks := ChunkText(content)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if chunks[0].Index != 0 || chunks[1].Index != 1 {
		t.Errorf("indices wrong: %+v", chunks)
	}
}

func TestChunkerNoBoundary(t *testing.T) {
	// 1000 chars, no newlines → first chunk = 800, next start = 700, so
	// second window = content[700:1000] = 300 chars → one more chunk.
	content := strings.Repeat("a", 1000)
	chunks := ChunkText(content)
	if len(chunks) != 2 {
		t.Fatalf("got %d chunks, want 2", len(chunks))
	}
	if len(chunks[0].Content) != 800 {
		t.Errorf("first chunk len %d, want 800", len(chunks[0].Content))
	}
	if len(chunks[1].Content) != 300 {
		t.Errorf("second chunk len %d, want 300", len(chunks[1].Content))
	}
}

// ---- router ----------------------------------------------------------------

func TestRouterFolderMatch(t *testing.T) {
	rooms := []room.Room{
		{Name: "frontend", Keywords: []string{"ui"}},
		{Name: "general"},
	}
	got := DetectRoom("frontend/app.js", "x", rooms)
	if got != "frontend" {
		t.Errorf("got %q, want frontend", got)
	}
}

func TestRouterFilenameMatch(t *testing.T) {
	rooms := []room.Room{{Name: "api"}, {Name: "general"}}
	got := DetectRoom("src/api.go", "x", rooms)
	if got != "api" {
		t.Errorf("got %q, want api", got)
	}
}

func TestRouterKeywordScore(t *testing.T) {
	rooms := []room.Room{
		{Name: "backend", Keywords: []string{"database"}},
		{Name: "frontend", Keywords: []string{"ui"}},
	}
	got := DetectRoom("util/helper.py", "database database ui", rooms)
	if got != "backend" {
		t.Errorf("got %q, want backend", got)
	}
}

func TestRouterFallsBackToGeneral(t *testing.T) {
	rooms := []room.Room{
		{Name: "frontend", Keywords: []string{"ui"}},
		{Name: "general"},
	}
	got := DetectRoom("util/helper.py", "nothing matches here at all", rooms)
	if got != "general" {
		t.Errorf("got %q, want general", got)
	}
}

// ---- gitignore -------------------------------------------------------------

func testMatcher(t *testing.T, body string) *GitignoreMatcher {
	t.Helper()
	baseDir := t.TempDir()
	return parseGitignore(baseDir, body)
}

func TestGitignoreAnchored(t *testing.T) {
	m := testMatcher(t, "/anchored.txt\n")
	dec := m.Matches(filepath.Join(m.baseDir, "anchored.txt"), false)
	if dec == nil || !*dec {
		t.Errorf("anchored.txt at root: got %v, want ignored", dec)
	}
	dec = m.Matches(filepath.Join(m.baseDir, "root", "anchored.txt"), false)
	if dec != nil && *dec {
		t.Errorf("root/anchored.txt: got %v, want not-ignored", dec)
	}
}

func TestGitignoreDirOnly(t *testing.T) {
	m := testMatcher(t, "build/\n")
	dec := m.Matches(filepath.Join(m.baseDir, "build"), true)
	if dec == nil || !*dec {
		t.Errorf("build (dir): got %v, want ignored", dec)
	}
	dec = m.Matches(filepath.Join(m.baseDir, "buildfile"), false)
	if dec != nil && *dec {
		t.Errorf("buildfile: got %v, want not-ignored", dec)
	}
}

func TestGitignoreDoubleStar(t *testing.T) {
	m := testMatcher(t, "**/secret.md\n")
	dec := m.Matches(filepath.Join(m.baseDir, "inner", "secret.md"), false)
	if dec == nil || !*dec {
		t.Errorf("inner/secret.md: got %v, want ignored", dec)
	}
	dec = m.Matches(filepath.Join(m.baseDir, "a", "b", "c", "secret.md"), false)
	if dec == nil || !*dec {
		t.Errorf("deep secret.md: got %v, want ignored", dec)
	}
}

func TestGitignoreNegation(t *testing.T) {
	m := testMatcher(t, "*.log\n!keep.log\n")
	// keep.log: last rule wins (negated) → not ignored.
	dec := m.Matches(filepath.Join(m.baseDir, "keep.log"), false)
	if dec == nil || *dec {
		t.Errorf("keep.log: got %v, want not-ignored", dec)
	}
	// a.log: only the first rule applies.
	dec = m.Matches(filepath.Join(m.baseDir, "a.log"), false)
	if dec == nil || !*dec {
		t.Errorf("a.log: got %v, want ignored", dec)
	}
}

func TestGitignoreEscapedHash(t *testing.T) {
	m := testMatcher(t, `\#foo`+"\n")
	dec := m.Matches(filepath.Join(m.baseDir, "#foo"), false)
	if dec == nil || !*dec {
		t.Errorf("#foo: got %v, want ignored", dec)
	}
}

// ---- scan ------------------------------------------------------------------

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanProjectSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "b.md"), "body body body body body body body body")
	writeFile(t, filepath.Join(dir, "a.md"), "body body body body body body body body")
	writeFile(t, filepath.Join(dir, "c.md"), "body body body body body body body body")

	files, err := ScanProject(dir, ScanOptions{RespectGitignore: true})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(files) != 3 {
		t.Fatalf("got %d files, want 3: %+v", len(files), files)
	}
	var bases []string
	for _, f := range files {
		bases = append(bases, filepath.Base(f))
	}
	if bases[0] != "a.md" || bases[1] != "b.md" || bases[2] != "c.md" {
		t.Errorf("order wrong: %+v", bases)
	}
}

func TestScanProjectRespectsGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "ignore.md\n")
	writeFile(t, filepath.Join(dir, "ignore.md"), "some body text long enough long enough long enough")
	writeFile(t, filepath.Join(dir, "keep.md"), "another body text long enough long enough long enough")

	files, err := ScanProject(dir, ScanOptions{RespectGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || filepath.Base(files[0]) != "keep.md" {
		t.Errorf("got %v, want [keep.md]", files)
	}
}

func TestScanProjectSkipsKnownDirs(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "node_modules", "x.js"), "skipped skipped skipped skipped skipped")
	writeFile(t, filepath.Join(dir, "src", "x.js"), "kept kept kept kept kept kept kept kept")
	files, err := ScanProject(dir, ScanOptions{RespectGitignore: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 {
		t.Fatalf("got %d files, want 1: %+v", len(files), files)
	}
	if !strings.Contains(files[0], "src") {
		t.Errorf("wrong file kept: %s", files[0])
	}
}

func TestScanProjectNoGitignore(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "*.log\n")
	writeFile(t, filepath.Join(dir, "a.md"), "body long enough body long enough body")
	writeFile(t, filepath.Join(dir, "b.md"), "body long enough body long enough body")

	files, err := ScanProject(dir, ScanOptions{RespectGitignore: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("got %d files, want 2 — .gitignore should be disabled", len(files))
	}
}

func TestScanProjectForceInclude(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ".gitignore"), "dist/*\n")
	writeFile(t, filepath.Join(dir, "dist", "bundle.js"), "content content content content content content content")

	files, err := ScanProject(dir, ScanOptions{
		RespectGitignore: true,
		IncludeIgnored:   []string{"dist/bundle.js"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 1 || !strings.Contains(files[0], "bundle.js") {
		t.Errorf("force-include failed: %+v", files)
	}
}

// ---- alreadyMined ----------------------------------------------------------

func TestAlreadyMinedMTime(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "palace.db")
	p, err := palace.Open(dbPath, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("palace.Open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	src := filepath.Join(t.TempDir(), "thing.md")
	writeFile(t, src, "body body body body body body body body body body")
	info, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	mtime := float64(info.ModTime().UnixNano()) / 1e9
	d := palace.Drawer{
		ID:          palace.ComputeDrawerID("w", "r", src, 0),
		Document:    "hello",
		Wing:        "w",
		Room:        "r",
		SourceFile:  src,
		ChunkIndex:  0,
		AddedBy:     "test",
		FiledAt:     time.Now(),
		SourceMTime: mtime,
	}
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if !alreadyMined(p, src, info) {
		t.Error("expected alreadyMined=true for matching mtime")
	}

	// Touch the file to force a new mtime.
	later := time.Now().Add(5 * time.Second)
	if err := os.Chtimes(src, later, later); err != nil {
		t.Fatal(err)
	}
	info2, _ := os.Stat(src)
	if alreadyMined(p, src, info2) {
		t.Error("expected alreadyMined=false after mtime bump")
	}
}
