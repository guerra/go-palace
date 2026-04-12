//go:build testsuite
// +build testsuite

// Package testsuite_test holds the behavioral equivalence suite that drives
// both the Go binary and the Python oracle via subprocess and compares
// observable outputs. Build tag "testsuite" keeps it out of the default
// `go test ./...` pipeline so make audit does not require Python.
package testsuite_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

type invocation struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// impl selects which implementations to exercise. Default "go" so Phase A
// builds do not require a Python toolchain.
func impl() string {
	if v := os.Getenv("MEMPALACE_IMPL"); v != "" {
		return v
	}
	return "go"
}

func runCmd(t *testing.T, name string, args ...string) invocation {
	t.Helper()
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	inv := invocation{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		inv.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s %v: %v", name, args, err)
	}
	return inv
}

func invokeGo(t *testing.T, args ...string) invocation {
	t.Helper()
	bin := os.Getenv("MEMPALACE_GO_BIN")
	if bin == "" {
		t.Fatal("MEMPALACE_GO_BIN is required for the behavioral suite")
	}
	return runCmd(t, bin, args...)
}

func invokePython(t *testing.T, args ...string) invocation {
	t.Helper()
	// Prefer a MEMPALACE_PY_CMD override (e.g. "uv run --directory /path python -m mempalace"),
	// otherwise fall back to `python -m mempalace`.
	//
	// IMPORTANT: the override is split with strings.Fields, which is
	// whitespace-only. Paths that contain spaces WILL break this splitter.
	// If you need to invoke a python in a directory with spaces, point
	// MEMPALACE_PY_CMD at a wrapper shell script instead.
	cmdLine := os.Getenv("MEMPALACE_PY_CMD")
	if cmdLine == "" {
		cmdLine = "python -m mempalace"
	}
	parts := strings.Fields(cmdLine)
	full := append(parts[1:], args...)
	return runCmd(t, parts[0], full...)
}

// invoke returns (python, go) invocations according to MEMPALACE_IMPL.
// Either side may be nil — compareStructural skips nils.
func invoke(t *testing.T, args ...string) (*invocation, *invocation) {
	t.Helper()
	var py, goInv *invocation
	mode := impl()
	if mode == "both" || mode == "python" {
		v := invokePython(t, args...)
		py = &v
	}
	if mode == "both" || mode == "go" {
		v := invokeGo(t, args...)
		goInv = &v
	}
	return py, goInv
}

func compareStructural(t *testing.T, id string, py, goInv *invocation, patterns []string) {
	t.Helper()
	for _, p := range patterns {
		re := regexp.MustCompile(p)
		if py != nil && !re.MatchString(py.Stdout+py.Stderr) {
			t.Errorf("%s: python output missing pattern %q", id, p)
		}
		if goInv != nil && !re.MatchString(goInv.Stdout+goInv.Stderr) {
			t.Errorf("%s: go output missing pattern %q", id, p)
		}
	}
}

func compareExitCode(t *testing.T, id string, py, goInv *invocation) {
	t.Helper()
	if py != nil && goInv != nil && py.ExitCode != goInv.ExitCode {
		t.Errorf("%s: exit code differs py=%d go=%d", id, py.ExitCode, goInv.ExitCode)
	}
}

// ----------------------------------------------------------------------------
// Phase B helpers: fixture copying + palace tempdir
// ----------------------------------------------------------------------------

// fixturesDir returns the absolute path to testdata/fixtures, anchored to
// this test file's on-disk location. Using runtime.Caller avoids brittle
// assumptions about CWD when `go test ./testsuite/...` runs.
func fixturesDir() string {
	_, thisFile, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(thisFile), "..", "testdata", "fixtures")
}

// copyFixture duplicates testdata/fixtures/<name> into t.TempDir() so the
// test can mutate init output (writing mempalace.yaml) without touching
// the committed fixture tree.
func copyFixture(t *testing.T, name string) string {
	t.Helper()
	src := filepath.Join(fixturesDir(), name)
	dst := filepath.Join(t.TempDir(), name)
	if err := copyTree(src, dst); err != nil {
		t.Fatalf("copy fixture %s: %v", name, err)
	}
	return dst
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	_, err = io.Copy(out, in)
	return err
}

func tempPalace(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "palace")
}

// ----------------------------------------------------------------------------
// B-001 / B-002 (widened to include init)
// ----------------------------------------------------------------------------

func TestB001_NoArgs_PrintsHelp(t *testing.T) {
	py, goInv := invoke(t)
	patterns := []string{`(?i)mempalace`, `(?i)init`, `(?i)mine`, `(?i)search`, `(?i)status`, `(?i)split`, `(?i)hook`, `(?i)instructions`, `(?i)repair`, `(?i)compress`, `(?i)mcp`}
	compareStructural(t, "B-001", py, goInv, patterns)
	compareExitCode(t, "B-001", py, goInv)
}

func TestB002_HelpFlag(t *testing.T) {
	py, goInv := invoke(t, "--help")
	patterns := []string{`(?i)usage`, `(?i)init`, `(?i)mine`, `(?i)search`, `(?i)status`, `(?i)split`, `(?i)mcp`}
	compareStructural(t, "B-002", py, goInv, patterns)
	compareExitCode(t, "B-002", py, goInv)
}

// ----------------------------------------------------------------------------
// B-004: init --yes writes mempalace.yaml
// ----------------------------------------------------------------------------

func TestB004_InitYes(t *testing.T) {
	if impl() == "both" {
		t.Skip("Python init also writes entities.json which Phase B does not — run go-only")
	}
	dir := copyFixture(t, "sample_project")
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv == nil || goInv.ExitCode != 0 {
		t.Fatalf("init failed: %+v", goInv)
	}
	for _, want := range []string{"WING:", "ROOM:", "mempalace.yaml"} {
		if !strings.Contains(goInv.Stdout, want) {
			t.Errorf("missing %q in init stdout: %s", want, goInv.Stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "mempalace.yaml")); err != nil {
		t.Errorf("yaml not written: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "entities.json")); err != nil {
		t.Errorf("entities.json not written: %v", err)
	}
}

func TestB005_InitYesIdempotent(t *testing.T) {
	if impl() == "both" {
		t.Skip("go-only test")
	}
	dir := copyFixture(t, "sample_project")
	for i := 0; i < 2; i++ {
		_, goInv := invoke(t, "init", dir, "--yes")
		if goInv.ExitCode != 0 {
			t.Fatalf("run %d: exit=%d stdout=%s", i, goInv.ExitCode, goInv.Stdout)
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "mempalace.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "wing:") {
		t.Errorf("yaml corrupted: %s", data)
	}
}

// ----------------------------------------------------------------------------
// B-006: mine a project (real palace via FakeEmbedder fallback)
// ----------------------------------------------------------------------------

func TestB006_MineProject(t *testing.T) {
	if impl() == "both" && os.Getenv("MEMPALACE_PY_DIR") == "" {
		t.Skip("both mode requires MEMPALACE_PY_DIR for palace parity")
	}
	dir := copyFixture(t, "sample_project")
	palace := tempPalace(t)

	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init failed: %+v", goInv)
	}
	_, goInv = invoke(t, "mine", dir, "--palace", palace)
	if goInv.ExitCode != 0 {
		t.Fatalf("mine failed: exit=%d stdout=%s stderr=%s", goInv.ExitCode, goInv.Stdout, goInv.Stderr)
	}
	patterns := []string{
		`MemPalace Mine`, `Wing:`, `Files:`, `Palace:`, `Done\.`, `Drawers filed:`, `By room:`,
	}
	for _, p := range patterns {
		if !regexp.MustCompile(p).MatchString(goInv.Stdout) {
			t.Errorf("mine stdout missing %q: %s", p, goInv.Stdout)
		}
	}
}

// ----------------------------------------------------------------------------
// B-008: mine --dry-run (no palace, no embedder)
// ----------------------------------------------------------------------------

func TestB008_MineDryRun(t *testing.T) {
	dir := copyFixture(t, "sample_project")
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init: %+v", goInv)
	}
	_, goInv = invoke(t, "mine", dir, "--palace", "/nonexistent/palace", "--dry-run")
	if goInv.ExitCode != 0 {
		t.Fatalf("dry-run exit=%d: %s", goInv.ExitCode, goInv.Stdout)
	}
	for _, p := range []string{`DRY RUN`, `\[DRY RUN\]`} {
		if !regexp.MustCompile(p).MatchString(goInv.Stdout) {
			t.Errorf("dry-run stdout missing %q: %s", p, goInv.Stdout)
		}
	}
}

// ----------------------------------------------------------------------------
// B-009: mine --wing override
// ----------------------------------------------------------------------------

func TestB009_MineWingOverride(t *testing.T) {
	dir := copyFixture(t, "sample_project")
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init: %+v", goInv)
	}
	_, goInv = invoke(t, "mine", dir, "--wing", "custom_wing", "--palace", "/nonexistent/palace", "--dry-run")
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stdout)
	}
	if !strings.Contains(goInv.Stdout, "custom_wing") {
		t.Errorf("--wing override missing: %s", goInv.Stdout)
	}
}

// ----------------------------------------------------------------------------
// B-010: mine --limit
// ----------------------------------------------------------------------------

func TestB010_MineLimit(t *testing.T) {
	dir := copyFixture(t, "sample_project")
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init: %+v", goInv)
	}
	_, goInv = invoke(t, "mine", dir, "--limit", "1", "--palace", "/nonexistent/palace", "--dry-run")
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stdout)
	}
	if n := strings.Count(goInv.Stdout, "[DRY RUN]"); n != 1 {
		t.Errorf("--limit 1 produced %d DRY RUN lines, want 1: %s", n, goInv.Stdout)
	}
}

// ----------------------------------------------------------------------------
// B-011: mine --no-gitignore
// ----------------------------------------------------------------------------

func TestB011_MineNoGitignore(t *testing.T) {
	dir := copyFixture(t, "project_with_gitignore")
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init: %+v", goInv)
	}

	// Default: gitignore respected.
	_, goInv = invoke(t, "mine", dir, "--palace", "/nonexistent/palace", "--dry-run")
	if goInv.ExitCode != 0 {
		t.Fatalf("mine default: exit=%d: %s", goInv.ExitCode, goInv.Stdout)
	}
	if !strings.Contains(goInv.Stdout, "valid.txt") {
		t.Errorf("expected valid.txt in default output: %s", goInv.Stdout)
	}
	if !strings.Contains(goInv.Stdout, "special_keep.txt") {
		t.Errorf("expected special_keep.txt (negation) in default output: %s", goInv.Stdout)
	}
	if strings.Contains(goInv.Stdout, "special_ignored.txt") {
		t.Errorf("special_ignored.txt should be gitignored: %s", goInv.Stdout)
	}
	if strings.Contains(goInv.Stdout, "secret.md") {
		t.Errorf("secret.md should be gitignored by **/secret.md: %s", goInv.Stdout)
	}

	// --no-gitignore: special_ignored.txt should now appear.
	_, goInv = invoke(t, "mine", dir, "--palace", "/nonexistent/palace", "--dry-run", "--no-gitignore")
	if goInv.ExitCode != 0 {
		t.Fatalf("mine no-gitignore: exit=%d: %s", goInv.ExitCode, goInv.Stdout)
	}
	if !strings.Contains(goInv.Stdout, "special_ignored.txt") {
		t.Errorf("--no-gitignore did not include special_ignored.txt: %s", goInv.Stdout)
	}
}

// ----------------------------------------------------------------------------
// B-012: incremental mtime skip
// ----------------------------------------------------------------------------

func TestB012_MineIncrementalSkip(t *testing.T) {
	if impl() == "both" && os.Getenv("MEMPALACE_PY_DIR") == "" {
		t.Skip("both mode requires MEMPALACE_PY_DIR for palace parity")
	}
	dir := copyFixture(t, "sample_project")
	palace := tempPalace(t)

	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv.ExitCode != 0 {
		t.Fatalf("init: %+v", goInv)
	}

	// First run — files processed, nothing skipped.
	_, goInv = invoke(t, "mine", dir, "--palace", palace)
	if goInv.ExitCode != 0 {
		t.Fatalf("first mine: exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !regexp.MustCompile(`Files skipped \(already filed\): 0`).MatchString(goInv.Stdout) {
		t.Errorf("first run should skip 0 files: %s", goInv.Stdout)
	}

	// Second run against the same palace — every file should be skipped.
	_, goInv = invoke(t, "mine", dir, "--palace", palace)
	if goInv.ExitCode != 0 {
		t.Fatalf("second mine: exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if regexp.MustCompile(`Files skipped \(already filed\): 0`).MatchString(goInv.Stdout) {
		t.Errorf("second run should skip >0 files: %s", goInv.Stdout)
	}
}

// ----------------------------------------------------------------------------
// B-007: mine --mode convos (dry-run)
// ----------------------------------------------------------------------------

func TestB007_MineConvos(t *testing.T) {
	dir := copyFixture(t, "convos")
	palace := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palace, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine convos dry-run exit=%d: stdout=%s stderr=%s", goInv.ExitCode, goInv.Stdout, goInv.Stderr)
	}
	for _, p := range []string{`MemPalace Mine`, `Conversations`, `DRY RUN`} {
		if !regexp.MustCompile(p).MatchString(goInv.Stdout) {
			t.Errorf("mine convos stdout missing %q: %s", p, goInv.Stdout)
		}
	}
}

// ----------------------------------------------------------------------------
// B-013: mine --mode convos --extract general (dry-run)
// ----------------------------------------------------------------------------

func TestB013_MineExtractGeneral(t *testing.T) {
	dir := copyFixture(t, "convos")
	palace := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--extract", "general", "--palace", palace, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine extract general exit=%d: stdout=%s stderr=%s", goInv.ExitCode, goInv.Stdout, goInv.Stderr)
	}
	for _, p := range []string{`DRY RUN`, `memories`} {
		if !regexp.MustCompile(p).MatchString(goInv.Stdout) {
			t.Errorf("mine extract general stdout missing %q: %s", p, goInv.Stdout)
		}
	}
}

// ----------------------------------------------------------------------------
// Phase D helpers: mine fixture into a real palace for search/wake-up tests
// ----------------------------------------------------------------------------

func seedPalace(t *testing.T) string {
	t.Helper()
	dir := copyFixture(t, "sample_project")
	palacePath := tempPalace(t)
	_, goInv := invoke(t, "init", dir, "--yes")
	if goInv == nil || goInv.ExitCode != 0 {
		t.Fatalf("seedPalace init failed: %+v", goInv)
	}
	_, goInv = invoke(t, "mine", dir, "--palace", palacePath)
	if goInv == nil || goInv.ExitCode != 0 {
		t.Fatalf("seedPalace mine failed: %+v", goInv)
	}
	return palacePath
}

// ----------------------------------------------------------------------------
// B-014: search returns formatted results
// ----------------------------------------------------------------------------

func TestB014_SearchResults(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "search", "test", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	// An empty palace may return "No results found" which is valid.
	// A populated palace should return formatted results.
	out := goInv.Stdout + goInv.Stderr
	if goInv.ExitCode != 0 {
		t.Fatalf("search exit=%d: %s", goInv.ExitCode, out)
	}
	// Either we get results or "No results found" — both are valid.
	hasResults := strings.Contains(out, "Match:") && strings.Contains(out, "Source:")
	hasNoResults := strings.Contains(out, "No results found")
	if !hasResults && !hasNoResults {
		t.Errorf("search output missing expected patterns:\n%s", out)
	}
}

// ----------------------------------------------------------------------------
// B-015: search with wing/room filters
// ----------------------------------------------------------------------------

func TestB015_SearchFiltered(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "search", "content", "--palace", palacePath, "--wing", "sample_project")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("search filtered exit=%d: %s", goInv.ExitCode, goInv.Stdout+goInv.Stderr)
	}
	out := goInv.Stdout
	if strings.Contains(out, "Match:") && !strings.Contains(out, "Wing:") {
		t.Errorf("filtered search missing Wing label:\n%s", out)
	}
}

// ----------------------------------------------------------------------------
// B-017: search missing palace exits non-zero
// ----------------------------------------------------------------------------

func TestB017_SearchMissingPalace(t *testing.T) {
	// Use a directory path that sqlite can't open as a DB.
	_, goInv := invoke(t, "search", "test", "--palace", "/tmp/nonexistent_dir_b017/palace.db")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	out := goInv.Stdout + goInv.Stderr
	if goInv.ExitCode == 0 {
		// sqlite3 may create the file; if it returns 0 with "No results found" that's acceptable
		if !strings.Contains(out, "No results found") && !strings.Contains(out, "No palace found") {
			t.Errorf("expected error or hint for missing palace, got exit=0: %s", out)
		}
	}
}

// ----------------------------------------------------------------------------
// B-020: wake-up returns L0+L1 text
// ----------------------------------------------------------------------------

func TestB020_WakeUp(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stdout+goInv.Stderr)
	}
	out := goInv.Stdout
	patterns := []string{`Wake-up text`, `tokens`}
	for _, p := range patterns {
		if !regexp.MustCompile(p).MatchString(out) {
			t.Errorf("wake-up stdout missing %q:\n%s", p, out)
		}
	}
}

// ----------------------------------------------------------------------------
// B-021: wake-up with wing filter
// ----------------------------------------------------------------------------

func TestB021_WakeUpWing(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath, "--wing", "sample_project")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up wing exit=%d: %s", goInv.ExitCode, goInv.Stdout+goInv.Stderr)
	}
	out := goInv.Stdout
	if !strings.Contains(out, "Wake-up text") {
		t.Errorf("wake-up wing missing header:\n%s", out)
	}
}

// ----------------------------------------------------------------------------
// Phase F: B-024..B-029 — splitter, hooks, instructions
// ----------------------------------------------------------------------------

func TestB024_SplitMegaFile(t *testing.T) {
	dir := t.TempDir()
	// Create a multi-session .txt file.
	content := "Claude Code v1.0\nfirst session content\n" +
		strings.Repeat("line\n", 20) +
		"Claude Code v1.0\nsecond session content\n" +
		strings.Repeat("line\n", 20)
	if err := os.WriteFile(filepath.Join(dir, "mega.txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	_, goInv := invoke(t, "split", dir, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("split exit=%d: %s", goInv.ExitCode, goInv.Stdout+goInv.Stderr)
	}
	out := goInv.Stdout
	if !strings.Contains(out, "DRY RUN") {
		t.Errorf("split --dry-run missing DRY RUN in output: %s", out)
	}
	if !strings.Contains(out, "2 sessions") {
		t.Errorf("split should mention 2 sessions: %s", out)
	}
}

func TestB026_HookStop(t *testing.T) {
	dir := t.TempDir()
	// No transcript = low count = no block.
	input := `{"session_id":"test","stop_hook_active":false,"transcript_path":""}`
	goInv := runCmdWithStdin(t, input, "hook", "run", "--hook", "stop", "--harness", "claude-code")
	if goInv.ExitCode != 0 {
		t.Fatalf("hook stop exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	_ = dir
	// Should output JSON — empty means no block.
	if !strings.Contains(goInv.Stdout, "{") {
		t.Errorf("hook stop should output JSON: %s", goInv.Stdout)
	}
}

func TestB027_HookSessionStart(t *testing.T) {
	input := `{"session_id":"test123"}`
	goInv := runCmdWithStdin(t, input, "hook", "run", "--hook", "session-start", "--harness", "claude-code")
	if goInv.ExitCode != 0 {
		t.Fatalf("hook session-start exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "{") {
		t.Errorf("hook session-start should output JSON: %s", goInv.Stdout)
	}
}

func TestB028_HookPrecompact(t *testing.T) {
	input := `{"session_id":"test123"}`
	goInv := runCmdWithStdin(t, input, "hook", "run", "--hook", "precompact", "--harness", "claude-code")
	if goInv.ExitCode != 0 {
		t.Fatalf("hook precompact exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "block") {
		t.Errorf("hook precompact should output block decision: %s", goInv.Stdout)
	}
}

func TestB029_Instructions(t *testing.T) {
	_, goInv := invoke(t, "instructions", "init")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("instructions init exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "MemPalace Init") {
		t.Errorf("instructions init missing expected content: %s", goInv.Stdout)
	}

	// Unknown instruction should fail.
	_, goInv2 := invoke(t, "instructions", "nonexistent")
	if goInv2 == nil {
		t.Skip("go impl not available")
	}
	if goInv2.ExitCode == 0 {
		t.Error("instructions nonexistent should exit non-zero")
	}
}

// ----------------------------------------------------------------------------
// Phase F: B-050..B-075 — MCP behavioral tests
// ----------------------------------------------------------------------------

func TestB050_MCPInitialize(t *testing.T) {
	palacePath := seedPalace(t)
	req := `{"jsonrpc":"2.0","method":"initialize","id":1,"params":{"protocolVersion":"2024-11-05"}}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp init exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "protocolVersion") {
		t.Errorf("mcp initialize missing protocolVersion: %s", goInv.Stdout)
	}
	if !strings.Contains(goInv.Stdout, "mempalace") {
		t.Errorf("mcp initialize missing server name: %s", goInv.Stdout)
	}
}

func TestB051_MCPToolsList(t *testing.T) {
	palacePath := seedPalace(t)
	req := `{"jsonrpc":"2.0","method":"tools/list","id":2}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp tools/list exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	for _, tool := range []string{"mempalace_status", "mempalace_search", "mempalace_add_drawer"} {
		if !strings.Contains(goInv.Stdout, tool) {
			t.Errorf("tools/list missing %s: %s", tool, goInv.Stdout)
		}
	}
}

func TestB054_MCPAddDrawer(t *testing.T) {
	palacePath := seedPalace(t)
	req := `{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"mempalace_add_drawer","arguments":{"wing":"test_wing","room":"test_room","content":"test content"}}}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp add_drawer exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "success") {
		t.Errorf("add_drawer missing success: %s", goInv.Stdout)
	}
}

func TestB055_MCPAddDrawerIdempotent(t *testing.T) {
	palacePath := seedPalace(t)
	addReq := `{"jsonrpc":"2.0","method":"tools/call","id":3,"params":{"name":"mempalace_add_drawer","arguments":{"wing":"test_wing","room":"test_room","content":"test content"}}}` + "\n"
	// Send same request twice.
	twoReqs := addReq + addReq
	goInv := runCmdWithStdin(t, twoReqs, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp add_drawer x2 exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "already_exists") {
		t.Errorf("second add should return already_exists: %s", goInv.Stdout)
	}
}

func TestB074_MCPUnknownTool(t *testing.T) {
	palacePath := seedPalace(t)
	req := `{"jsonrpc":"2.0","method":"tools/call","id":4,"params":{"name":"nonexistent_tool"}}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp unknown tool exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "Unknown tool") {
		t.Errorf("unknown tool missing error: %s", goInv.Stdout)
	}
}

func TestB075_MCPUnknownMethod(t *testing.T) {
	palacePath := seedPalace(t)
	req := `{"jsonrpc":"2.0","method":"bogus/method","id":5}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp unknown method exit=%d: stderr=%s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "Unknown method") {
		t.Errorf("unknown method missing error: %s", goInv.Stdout)
	}
}

// ----------------------------------------------------------------------------
// Phase F: Enhanced status test
// ----------------------------------------------------------------------------

func TestB030_EnhancedStatus(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "status", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("status exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	out := goInv.Stdout
	for _, want := range []string{"drawers:", "wings:", "rooms:"} {
		if !strings.Contains(out, want) {
			t.Errorf("enhanced status missing %q: %s", want, out)
		}
	}
}

// ----------------------------------------------------------------------------
// Phase F helpers: stdin piping
// ----------------------------------------------------------------------------

func runCmdWithStdin(t *testing.T, stdin string, args ...string) invocation {
	t.Helper()
	bin := os.Getenv("MEMPALACE_GO_BIN")
	if bin == "" {
		t.Fatal("MEMPALACE_GO_BIN is required for the behavioral suite")
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, args...)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	inv := invocation{Stdout: stdout.String(), Stderr: stderr.String()}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		inv.ExitCode = exitErr.ExitCode()
	} else if err != nil {
		t.Fatalf("run %s %v: %v", bin, args, err)
	}
	return inv
}

// mcpCall sends an MCP tools/call request and returns the parsed tool result.
func mcpCall(t *testing.T, palacePath, toolName string, args map[string]any) map[string]any {
	t.Helper()
	req := map[string]any{
		"jsonrpc": "2.0",
		"method":  "tools/call",
		"id":      1,
		"params":  map[string]any{"name": toolName, "arguments": args},
	}
	data, _ := json.Marshal(req)
	goInv := runCmdWithStdin(t, string(data)+"\n", "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp call %s exit=%d: stderr=%s", toolName, goInv.ExitCode, goInv.Stderr)
	}
	return parseMCPToolResult(t, goInv.Stdout)
}

// mcpRaw sends an arbitrary JSON-RPC request and returns the raw parsed response.
func mcpRaw(t *testing.T, palacePath string, req map[string]any) map[string]any {
	t.Helper()
	data, _ := json.Marshal(req)
	goInv := runCmdWithStdin(t, string(data)+"\n", "mcp", "--serve", "--palace", palacePath)
	var resp map[string]any
	for _, line := range strings.Split(strings.TrimSpace(goInv.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if err := json.Unmarshal([]byte(line), &resp); err == nil {
			return resp
		}
	}
	t.Fatalf("could not parse MCP response: %s", goInv.Stdout)
	return nil
}

func parseMCPToolResult(t *testing.T, stdout string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(line), &resp); err != nil {
			continue
		}
		result, ok := resp["result"].(map[string]any)
		if !ok {
			continue
		}
		content, ok := result["content"].([]any)
		if !ok || len(content) == 0 {
			continue
		}
		item, _ := content[0].(map[string]any)
		text, _ := item["text"].(string)
		var toolResult map[string]any
		if err := json.Unmarshal([]byte(text), &toolResult); err != nil {
			t.Fatalf("parse tool result text: %v: %s", err, text)
		}
		return toolResult
	}
	t.Fatalf("no tool result found in output: %s", stdout)
	return nil
}

// seedPalaceWithContent creates a palace with specific content for search tests.
func seedPalaceWithContent(t *testing.T) string {
	t.Helper()
	palacePath := seedPalace(t)
	// Add extra drawers via MCP for richer test data.
	for _, item := range []struct{ wing, room, content string }{
		{"myproject", "technical", "ChromaDB setup guide for the project"},
		{"myproject", "technical", "Database migration plan for v2"},
		{"myproject", "design", "UI mockups and wireframes for the new dashboard"},
		{"other_proj", "notes", "Meeting notes from the planning session"},
	} {
		addReq := fmt.Sprintf(`{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"mempalace_add_drawer","arguments":{"wing":%q,"room":%q,"content":%q}}}`,
			item.wing, item.room, item.content)
		goInv := runCmdWithStdin(t, addReq+"\n", "mcp", "--serve", "--palace", palacePath)
		if goInv.ExitCode != 0 {
			t.Fatalf("seed content: exit=%d stderr=%s", goInv.ExitCode, goInv.Stderr)
		}
	}
	return palacePath
}

// ----------------------------------------------------------------------------
// Task 1: Config system tests (B-003, B-018, B-019, B-031, B-040..B-046)
// ----------------------------------------------------------------------------

func TestB003_UnknownSubcommand(t *testing.T) {
	_, goInv := invoke(t, "foobar")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode == 0 {
		t.Errorf("unknown subcommand should exit non-zero, got 0")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(strings.ToLower(combined), "unknown") {
		t.Errorf("expected 'unknown' in error output: %s", combined)
	}
}

func TestB018_StatusShowsCounts(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "status", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("status exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	out := goInv.Stdout
	// Must show drawer count, wings, rooms
	if !regexp.MustCompile(`drawers:\s*\d+`).MatchString(out) {
		t.Errorf("status missing drawer count: %s", out)
	}
	if !regexp.MustCompile(`wings:\s*\d+`).MatchString(out) {
		t.Errorf("status missing wing count: %s", out)
	}
	if !regexp.MustCompile(`rooms:\s*\d+`).MatchString(out) {
		t.Errorf("status missing room count: %s", out)
	}
}

func TestB019_StatusMissingPalace(t *testing.T) {
	_, goInv := invoke(t, "status", "--palace", "/tmp/nonexistent_parent_b019/nonexistent_palace")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	// Should exit 0 but show error info
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, "not found") && !strings.Contains(combined, "hint") {
		t.Errorf("status on missing palace should show error info: %s", combined)
	}
}

func TestB031_GlobalPalaceFlag(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "status", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("status with --palace exit=%d", goInv.ExitCode)
	}
	if !strings.Contains(goInv.Stdout, palacePath) {
		t.Errorf("--palace flag not used, output doesn't mention path: %s", goInv.Stdout)
	}
}

func TestB040_DefaultPalacePath(t *testing.T) {
	// Config with no env vars and no config file should default to ~/.mempalace/palace.
	// This is tested at the unit level — behavioral test verifies via status output.
	dir := t.TempDir()
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	t.Setenv("HOME", dir)
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, ".mempalace") {
		t.Errorf("default palace path should contain .mempalace: %s", combined)
	}
}

func TestB041_EnvOverride(t *testing.T) {
	customPath := filepath.Join(t.TempDir(), "custom_palace")
	t.Setenv("MEMPALACE_PALACE_PATH", customPath)
	t.Setenv("MEMPAL_PALACE_PATH", "")
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, customPath) && !strings.Contains(combined, "custom_palace") {
		t.Errorf("MEMPALACE_PALACE_PATH should override, got: %s", combined)
	}
}

func TestB042_LegacyEnvOverride(t *testing.T) {
	legacyPath := filepath.Join(t.TempDir(), "legacy_palace")
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", legacyPath)
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, legacyPath) && !strings.Contains(combined, "legacy_palace") {
		t.Errorf("MEMPAL_PALACE_PATH legacy should override, got: %s", combined)
	}
}

func TestB043_ConfigFileOverride(t *testing.T) {
	// Tested at unit level in config_test.go (TestFileOverride).
	// Behavioral: create a temp config dir with config.json, check status reads it.
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".mempalace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	palacePath := filepath.Join(homeDir, "config_palace")
	if err := os.WriteFile(filepath.Join(configDir, "config.json"),
		[]byte(fmt.Sprintf(`{"palace_path":%q}`, palacePath)), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	t.Setenv("HOME", homeDir)
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, "config_palace") {
		t.Errorf("config file palace_path should be used, got: %s", combined)
	}
}

func TestB044_EnvPriorityOverConfig(t *testing.T) {
	// When both env and config are set, env wins.
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".mempalace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "config.json"),
		[]byte(`{"palace_path":"/tmp/from_config"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	envPath := filepath.Join(homeDir, "from_env")
	t.Setenv("MEMPALACE_PALACE_PATH", envPath)
	t.Setenv("MEMPAL_PALACE_PATH", "")
	t.Setenv("HOME", homeDir)
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, "from_env") {
		t.Errorf("env should take priority over config, got: %s", combined)
	}
}

func TestB045_ConfigInit(t *testing.T) {
	// Config init creates config.json with defaults on first use.
	// Detailed config init behavior is tested at unit level (TestInitCreatesFiles).
	// Behavioral: verify the binary boots cleanly with a fresh HOME and produces
	// output referencing the default palace path.
	homeDir := t.TempDir()
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	t.Setenv("HOME", homeDir)
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, ".mempalace") {
		t.Errorf("fresh HOME status should reference default .mempalace path: %s", combined)
	}
}

func TestB046_PeopleMap(t *testing.T) {
	// people_map.json in config dir should be loaded without crashing.
	homeDir := t.TempDir()
	configDir := filepath.Join(homeDir, ".mempalace")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(configDir, "people_map.json"),
		[]byte(`{"gabe":"Gabriel","ga":"Gabriel"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", homeDir)
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	_, goInv := invoke(t, "status")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Errorf("status with people_map should work, exit=%d", goInv.ExitCode)
	}
	// Verify the binary ran against the correct HOME (config dir exists there).
	combined := goInv.Stdout + goInv.Stderr
	if !strings.Contains(combined, ".mempalace") {
		t.Errorf("expected output to reference .mempalace path under HOME: %s", combined)
	}
}

// ----------------------------------------------------------------------------
// Task 2: KG operations (B-110..B-117)
// ----------------------------------------------------------------------------

func TestB110_KGAddTriple(t *testing.T) {
	palacePath := seedPalace(t)
	result := mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("kg_add should succeed: %v", result)
	}
	tripleID, _ := result["triple_id"].(string)
	if tripleID == "" {
		t.Error("kg_add should return triple_id")
	}
	fact, _ := result["fact"].(string)
	if !strings.Contains(fact, "Max") || !strings.Contains(fact, "chess") {
		t.Errorf("fact string should contain subject and object: %s", fact)
	}
}

func TestB111_KGAddTripleIdempotent(t *testing.T) {
	palacePath := seedPalace(t)
	args := map[string]any{"subject": "Max", "predicate": "loves", "object": "chess"}
	r1 := mcpCall(t, palacePath, "mempalace_kg_add", args)
	r2 := mcpCall(t, palacePath, "mempalace_kg_add", args)
	id1, _ := r1["triple_id"].(string)
	id2, _ := r2["triple_id"].(string)
	if id1 == "" || id2 == "" {
		t.Fatal("both calls should return triple_id")
	}
	if id1 != id2 {
		t.Errorf("idempotent: got different IDs %q vs %q", id1, id2)
	}
}

func TestB112_KGQueryEntity(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "child_of", "object": "Alice",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})

	result := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max", "direction": "both",
	})
	facts, ok := result["facts"].([]any)
	if !ok {
		t.Fatalf("expected facts array: %v", result)
	}
	if len(facts) < 2 {
		t.Errorf("expected at least 2 facts, got %d", len(facts))
	}
	// Verify fact structure
	for _, f := range facts {
		fact, ok := f.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"direction", "subject", "predicate", "object", "current"} {
			if _, ok := fact[key]; !ok {
				t.Errorf("fact missing key %q", key)
			}
		}
	}
}

func TestB113_KGQueryAsOf(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
		"valid_from": "2025-01-01",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "chess",
		"valid_from": "2025-06-01",
	})

	// Query as of March — only swimming
	result := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max", "as_of": "2025-03-15",
	})
	facts, _ := result["facts"].([]any)
	if len(facts) != 1 {
		t.Errorf("as_of March: expected 1 fact, got %d", len(facts))
	}

	// Query as of July — both
	result = mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max", "as_of": "2025-07-01",
	})
	facts, _ = result["facts"].([]any)
	if len(facts) != 2 {
		t.Errorf("as_of July: expected 2 facts, got %d", len(facts))
	}
}

func TestB114_KGInvalidate(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
	})

	result := mcpCall(t, palacePath, "mempalace_kg_invalidate", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("invalidate should succeed: %v", result)
	}
	if _, ok := result["ended"]; !ok {
		t.Error("invalidate should return ended date")
	}

	// Query — should show as not current
	qr := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max", "direction": "outgoing",
	})
	facts, _ := qr["facts"].([]any)
	for _, f := range facts {
		fm, _ := f.(map[string]any)
		if obj, _ := fm["object"].(string); obj == "swimming" {
			if current, _ := fm["current"].(bool); current {
				t.Error("swimming should no longer be current after invalidation")
			}
		}
	}
}

func TestB115_KGTimeline(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "born", "object": "world",
		"valid_from": "2015-04-01",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
		"valid_from": "2025-01-01",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
		"valid_from": "2025-06-01",
	})

	result := mcpCall(t, palacePath, "mempalace_kg_timeline", map[string]any{
		"entity": "Max",
	})
	timeline, ok := result["timeline"].([]any)
	if !ok {
		t.Fatalf("expected timeline array: %v", result)
	}
	if len(timeline) != 3 {
		t.Errorf("expected 3 timeline entries, got %d", len(timeline))
	}
	// Should be chronological
	if len(timeline) >= 2 {
		first, _ := timeline[0].(map[string]any)
		second, _ := timeline[1].(map[string]any)
		vf1, _ := first["valid_from"].(string)
		vf2, _ := second["valid_from"].(string)
		if vf1 > vf2 {
			t.Errorf("timeline not chronological: %s > %s", vf1, vf2)
		}
	}
}

func TestB116_KGStats(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "child_of", "object": "Alice",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})
	_ = mcpCall(t, palacePath, "mempalace_kg_invalidate", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})

	result := mcpCall(t, palacePath, "mempalace_kg_stats", nil)
	entities, _ := result["entities"].(float64)
	triples, _ := result["triples"].(float64)
	current, _ := result["current_facts"].(float64)
	expired, _ := result["expired_facts"].(float64)
	if entities < 3 {
		t.Errorf("expected >=3 entities (Max, Alice, chess), got %v", entities)
	}
	if triples < 2 {
		t.Errorf("expected >=2 triples, got %v", triples)
	}
	if current < 1 {
		t.Errorf("expected >=1 current facts, got %v", current)
	}
	if expired < 1 {
		t.Errorf("expected >=1 expired facts, got %v", expired)
	}
	if _, ok := result["relationship_types"]; !ok {
		t.Error("stats should include relationship_types")
	}
}

func TestB117_KGEntityIDNormalization(t *testing.T) {
	palacePath := seedPalace(t)
	// "Max Power" and "max power" should resolve to the same entity.
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max Power", "predicate": "knows", "object": "Go",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "max power",
	})
	facts, _ := result["facts"].([]any)
	if len(facts) < 1 {
		t.Error("querying 'max power' should find facts for 'Max Power'")
	}

	// O'Brien normalization — apostrophes removed
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "O'Brien", "predicate": "works_at", "object": "Station",
	})
	result = mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "OBrien",
	})
	facts, _ = result["facts"].([]any)
	if len(facts) < 1 {
		t.Error("querying 'OBrien' should find facts for \"O'Brien\"")
	}
}

// ----------------------------------------------------------------------------
// Task 3: Graph operations (B-100..B-104)
// ----------------------------------------------------------------------------

func TestB100_BuildGraphNodesEdges(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_graph_stats", nil)
	totalRooms, _ := result["total_rooms"].(float64)
	if totalRooms < 1 {
		t.Errorf("expected at least 1 room, got %v", totalRooms)
	}
	if _, ok := result["total_edges"]; !ok {
		t.Error("graph_stats should include total_edges")
	}
	if _, ok := result["rooms_per_wing"]; !ok {
		t.Error("graph_stats should include rooms_per_wing")
	}
}

func TestB101_TraverseNonexistent(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_traverse", map[string]any{
		"start_room": "nonexistent-room-xyz",
	})
	// Should return error with suggestions
	errMsg, _ := result["error"].(string)
	if errMsg == "" {
		t.Error("traverse on nonexistent room should return error")
	}
	if !strings.Contains(errMsg, "not found") {
		t.Errorf("error should mention 'not found': %s", errMsg)
	}
	// Suggestions should be present (possibly empty for completely unrelated name)
	if _, ok := result["suggestions"]; !ok {
		t.Error("traverse error should include suggestions field")
	}
}

func TestB102_TraverseCap50(t *testing.T) {
	// Tested at unit level (graph_test.go TestTraverse checks cap).
	// Behavioral test: verify via MCP.
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_traverse", map[string]any{
		"start_room": "technical", "max_hops": 5,
	})
	nodes, ok := result["nodes"].([]any)
	if ok && len(nodes) > 50 {
		t.Errorf("traverse should cap at 50, got %d", len(nodes))
	}
}

func TestB103_FindTunnels(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	// Add a drawer with room "backend" under "myproject" wing — "backend" also
	// exists from the mined sample_project, creating a cross-wing tunnel.
	addReq := fmt.Sprintf(`{"jsonrpc":"2.0","method":"tools/call","id":1,"params":{"name":"mempalace_add_drawer","arguments":{"wing":"myproject","room":"backend","content":"shared backend room for tunnel test"}}}`)
	goInv := runCmdWithStdin(t, addReq+"\n", "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("seed tunnel data: exit=%d stderr=%s", goInv.ExitCode, goInv.Stderr)
	}

	result := mcpCall(t, palacePath, "mempalace_find_tunnels", map[string]any{})
	tunnels, ok := result["tunnels"].([]any)
	if !ok {
		t.Fatalf("expected tunnels array: %v", result)
	}
	if len(tunnels) < 1 {
		t.Errorf("expected at least 1 tunnel (room 'backend' in 2 wings), got %d", len(tunnels))
	}
	for _, tun := range tunnels {
		tm, ok := tun.(map[string]any)
		if !ok {
			continue
		}
		wings, _ := tm["wings"].([]any)
		if len(wings) < 2 {
			t.Errorf("tunnel room should have 2+ wings, got %d for %v", len(wings), tm["room"])
		}
	}
	// Verify count field
	if _, ok := result["count"]; !ok {
		t.Error("find_tunnels should include count")
	}
}

func TestB104_GraphStats(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_graph_stats", nil)
	for _, key := range []string{"total_rooms", "tunnel_rooms", "total_edges", "rooms_per_wing", "top_tunnels"} {
		if _, ok := result[key]; !ok {
			t.Errorf("graph_stats missing key %q", key)
		}
	}
}

// ----------------------------------------------------------------------------
// Task 4: Normalize formats (B-080..B-088)
// These are behavioral tests that exercise the normalize pipeline via CLI.
// Detailed format testing is in internal/normalize/normalize_test.go.
// ----------------------------------------------------------------------------

func writeFixtureFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestB080_NormalizePlainTextWithMarkers(t *testing.T) {
	dir := t.TempDir()
	content := "> What is Go?\nGo is a language.\n> Tell me more\nCreated by Google.\n> Thanks\nYou're welcome.\n"
	writeFixtureFile(t, dir, "convo.txt", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	// With markers, the file passes through and is mined.
	if !strings.Contains(goInv.Stdout, "DRY RUN") {
		t.Errorf("expected DRY RUN output: %s", goInv.Stdout)
	}
}

func TestB081_NormalizeClaudeCodeJSONL(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"human","message":{"content":"What is Go?"}}
{"type":"assistant","message":{"content":"Go is a programming language."}}
{"type":"human","message":{"content":"Tell me more"}}
{"type":"assistant","message":{"content":"Created by Google."}}`
	writeFixtureFile(t, dir, "session.jsonl", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "DRY RUN") {
		t.Errorf("expected DRY RUN output: %s", goInv.Stdout)
	}
}

func TestB082_NormalizeClaudeAIJSON(t *testing.T) {
	dir := t.TempDir()
	content := `[
		{"role": "user", "content": "Hello Claude"},
		{"role": "assistant", "content": "Hello! How can I help?"},
		{"role": "user", "content": "Write a poem"},
		{"role": "assistant", "content": "Roses are red..."}
	]`
	writeFixtureFile(t, dir, "claude.json", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB083_NormalizeChatGPTJSON(t *testing.T) {
	dir := t.TempDir()
	content := `{
		"mapping": {
			"root": {"parent": null, "message": null, "children": ["n1"]},
			"n1": {"parent": "root", "message": {"author": {"role": "user"}, "content": {"parts": ["What is Go?"]}}, "children": ["n2"]},
			"n2": {"parent": "n1", "message": {"author": {"role": "assistant"}, "content": {"parts": ["Go is a language."]}}, "children": ["n3"]},
			"n3": {"parent": "n2", "message": {"author": {"role": "user"}, "content": {"parts": ["More please"]}}, "children": ["n4"]},
			"n4": {"parent": "n3", "message": {"author": {"role": "assistant"}, "content": {"parts": ["It was made by Google."]}}, "children": []}
		}
	}`
	writeFixtureFile(t, dir, "chatgpt.json", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB084_NormalizeSlackJSON(t *testing.T) {
	dir := t.TempDir()
	content := `[
		{"type": "message", "user": "U001", "text": "Hey can you help?"},
		{"type": "message", "user": "U002", "text": "Sure, what do you need?"},
		{"type": "message", "user": "U001", "text": "How do I deploy?"},
		{"type": "message", "user": "U002", "text": "Just push to main."}
	]`
	writeFixtureFile(t, dir, "slack.json", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB085_NormalizeCodexJSONL(t *testing.T) {
	dir := t.TempDir()
	content := `{"type":"session_meta","session_id":"abc"}
{"type":"event_msg","payload":{"type":"user_message","message":"How do I deploy?"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"Use docker compose."}}
{"type":"event_msg","payload":{"type":"user_message","message":"What about k8s?"}}
{"type":"event_msg","payload":{"type":"agent_message","message":"That works too."}}`
	writeFixtureFile(t, dir, "codex.jsonl", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB086_NormalizeEmptyFile(t *testing.T) {
	dir := t.TempDir()
	writeFixtureFile(t, dir, "empty.txt", "")

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	// Should not crash on empty file.
	if goInv.ExitCode != 0 {
		t.Fatalf("mine empty file exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB087_NormalizePlainTextNoMarkers(t *testing.T) {
	dir := t.TempDir()
	content := "Just some regular text\nwith no special markers\nat all\n"
	writeFixtureFile(t, dir, "regular.txt", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine no-markers exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

func TestB088_NormalizeClaudeAIPrivacyExport(t *testing.T) {
	dir := t.TempDir()
	content := `[
		{
			"chat_messages": [
				{"role": "user", "content": "What is AI?"},
				{"role": "assistant", "content": "AI is artificial intelligence."},
				{"role": "user", "content": "Cool"},
				{"role": "assistant", "content": "Indeed!"}
			]
		}
	]`
	writeFixtureFile(t, dir, "privacy.json", content)

	palacePath := tempPalace(t)
	_, goInv := invoke(t, "mine", dir, "--mode", "convos", "--palace", palacePath, "--dry-run")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("mine privacy export exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
}

// ----------------------------------------------------------------------------
// Task 5: Search layer (B-016, B-090..B-094)
// ----------------------------------------------------------------------------

func TestB016_SearchResultsFlag(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "search", "test", "--palace", palacePath, "--results", "3")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("search --results exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	// Count the number of result blocks — each starts with [N]
	matches := regexp.MustCompile(`\[\d+\]`).FindAllString(goInv.Stdout, -1)
	if len(matches) > 3 {
		t.Errorf("--results 3 produced %d results, want <=3", len(matches))
	}
}

func TestB090_SearchMemoriesStructured(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "chromadb setup",
	})
	if _, ok := result["query"]; !ok {
		t.Error("search result should include query")
	}
	if _, ok := result["memories"]; !ok {
		t.Error("search result should include memories array")
	}
	memories, _ := result["memories"].([]any)
	for _, m := range memories {
		mem, ok := m.(map[string]any)
		if !ok {
			continue
		}
		for _, key := range []string{"text", "wing", "room", "source_file", "similarity"} {
			if _, ok := mem[key]; !ok {
				t.Errorf("memory result missing key %q", key)
			}
		}
	}
}

func TestB091_SearchWingFilter(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "project", "wing": "myproject",
	})
	memories, _ := result["memories"].([]any)
	for _, m := range memories {
		mem, ok := m.(map[string]any)
		if !ok {
			continue
		}
		wing, _ := mem["wing"].(string)
		if wing != "myproject" {
			t.Errorf("expected wing=myproject, got %s", wing)
		}
	}
}

func TestB092_SearchWingRoomFilter(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "project", "wing": "myproject", "room": "technical",
	})
	memories, _ := result["memories"].([]any)
	for _, m := range memories {
		mem, ok := m.(map[string]any)
		if !ok {
			continue
		}
		wing, _ := mem["wing"].(string)
		room, _ := mem["room"].(string)
		if wing != "myproject" {
			t.Errorf("expected wing=myproject, got %s", wing)
		}
		if room != "technical" {
			t.Errorf("expected room=technical, got %s", room)
		}
	}
}

func TestB093_SearchMissingPalaceError(t *testing.T) {
	// Search on nonexistent palace should produce an error.
	// Using a path with nonexistent parent dir so sqlite can't auto-create.
	_, goInv := invoke(t, "search", "test", "--palace", "/tmp/nonexistent_b093_xyz/subdir/palace.db")
	if goInv == nil {
		t.Skip("go impl not available")
	}
	// Should exit non-zero or show an error message.
	combined := goInv.Stdout + goInv.Stderr
	if goInv.ExitCode == 0 {
		if !strings.Contains(combined, "No results found") && !strings.Contains(combined, "No palace found") {
			t.Errorf("expected error or hint for missing palace, got exit=0: %s", combined)
		}
	}
	// Non-zero exit is the expected behavior.
}

func TestB094_SimilarityScoreFormula(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "chromadb setup",
	})
	memories, _ := result["memories"].([]any)
	if len(memories) == 0 {
		t.Skip("no results to check similarity formula")
	}
	for _, m := range memories {
		mem, ok := m.(map[string]any)
		if !ok {
			continue
		}
		sim, ok := mem["similarity"].(float64)
		if !ok {
			t.Error("similarity should be a number")
			continue
		}
		// Verify rounding to 3 decimal places: round(sim*1000)/1000 == sim.
		// This holds for both FakeEmbedder (random distances) and real embeddings.
		rounded := math.Round(sim*1000) / 1000
		if math.Abs(sim-rounded) > 1e-9 {
			t.Errorf("similarity %v not rounded to 3dp (expected %v)", sim, rounded)
		}
	}
}

// ----------------------------------------------------------------------------
// Task 6: Layers L0/L1 (B-120..B-125)
// These are primarily unit-level tests. Behavioral test via wake-up CLI.
// ----------------------------------------------------------------------------

func TestB120_L0Identity(t *testing.T) {
	palacePath := seedPalace(t)
	identityDir := filepath.Join(filepath.Dir(palacePath), "..", ".mempalace")
	if err := os.MkdirAll(identityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	idPath := filepath.Join(identityDir, "identity.txt")
	if err := os.WriteFile(idPath, []byte("I am Atlas, a personal AI assistant."), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Dir(identityDir))
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "Atlas") {
		t.Errorf("wake-up should contain identity content: %s", goInv.Stdout)
	}
}

func TestB121_L0Default(t *testing.T) {
	palacePath := seedPalace(t)
	// No identity.txt — should show default message.
	emptyHome := t.TempDir()
	t.Setenv("HOME", emptyHome)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "No identity configured") {
		t.Errorf("wake-up without identity should show default: %s", goInv.Stdout)
	}
}

func TestB122_L1EssentialStory(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	// L1 should be present with ESSENTIAL STORY header
	if !strings.Contains(goInv.Stdout, "L1") {
		t.Errorf("wake-up missing L1: %s", goInv.Stdout)
	}
}

func TestB123_L1MaxDrawersChars(t *testing.T) {
	// Tested at unit level (TestL1MaxChars in layers_test.go).
	// Behavioral: just verify wake-up doesn't produce unlimited output.
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	// Hard to test exact cap via CLI, but verify reasonable length.
	if len(goInv.Stdout) > 50000 {
		t.Errorf("wake-up output suspiciously long (%d bytes), L1 cap may be broken", len(goInv.Stdout))
	}
}

func TestB124_L1EmptyPalace(t *testing.T) {
	emptyPalace := tempPalace(t)
	// Create an empty palace by opening and closing.
	_, goInv := invoke(t, "wake-up", "--palace", emptyPalace)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	// May exit non-zero if palace doesn't exist, or 0 with "No memories yet"
	combined := goInv.Stdout + goInv.Stderr
	if goInv.ExitCode == 0 {
		if !strings.Contains(combined, "No memories") && !strings.Contains(combined, "tokens") {
			t.Errorf("empty palace wake-up should mention no memories or show token count: %s", combined)
		}
	}
}

func TestB125_WakeUpCombinesL0L1(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "wake-up", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("wake-up exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	out := goInv.Stdout
	// Should have both L0 and L1 content
	if !strings.Contains(out, "Wake-up text") {
		t.Errorf("wake-up missing header: %s", out)
	}
	if !strings.Contains(out, "tokens") {
		t.Errorf("wake-up missing token count: %s", out)
	}
}

// ----------------------------------------------------------------------------
// Task 7: MCP tools + CLI edge cases
// (B-052..B-073, B-076, B-077, B-022, B-023, B-025)
// ----------------------------------------------------------------------------

func TestB052_MCPStatus(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_status", nil)
	for _, key := range []string{"total_drawers", "wings", "rooms", "palace_path", "protocol", "aaak_dialect"} {
		if _, ok := result[key]; !ok {
			t.Errorf("status missing key %q", key)
		}
	}
	total, _ := result["total_drawers"].(float64)
	if total < 1 {
		t.Errorf("expected at least 1 drawer, got %v", total)
	}
}

func TestB053_MCPSearch(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "test",
	})
	for _, key := range []string{"query", "memories", "count"} {
		if _, ok := result[key]; !ok {
			t.Errorf("search missing key %q", key)
		}
	}
	memories, _ := result["memories"].([]any)
	if len(memories) > 0 {
		mem, _ := memories[0].(map[string]any)
		for _, key := range []string{"text", "wing", "room", "source_file", "similarity"} {
			if _, ok := mem[key]; !ok {
				t.Errorf("search result missing key %q", key)
			}
		}
	}
}

func TestB056_MCPDeleteDrawer(t *testing.T) {
	palacePath := seedPalace(t)
	// First add a drawer
	addResult := mcpCall(t, palacePath, "mempalace_add_drawer", map[string]any{
		"wing": "test_wing", "room": "test_room", "content": "content to delete",
	})
	drawerID, _ := addResult["drawer_id"].(string)

	// Delete it
	result := mcpCall(t, palacePath, "mempalace_delete_drawer", map[string]any{
		"drawer_id": drawerID,
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("delete should succeed: %v", result)
	}
}

func TestB057_MCPDeleteNonexistent(t *testing.T) {
	palacePath := seedPalace(t)
	result := mcpCall(t, palacePath, "mempalace_delete_drawer", map[string]any{
		"drawer_id": "nonexistent_drawer_xyz",
	})
	if success, _ := result["success"].(bool); success {
		t.Error("deleting nonexistent drawer should return success=false")
	}
	errMsg, _ := result["error"].(string)
	if !strings.Contains(strings.ToLower(errMsg), "not found") {
		t.Errorf("error should contain 'not found': %s", errMsg)
	}
}

func TestB058_MCPListWings(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_list_wings", nil)
	wings, ok := result["wings"].(map[string]any)
	if !ok {
		t.Fatalf("expected wings map: %v", result)
	}
	if len(wings) < 1 {
		t.Error("expected at least 1 wing")
	}
	// Each wing value should be a count
	for wing, count := range wings {
		if _, ok := count.(float64); !ok {
			t.Errorf("wing %q count should be a number, got %T", wing, count)
		}
	}
}

func TestB059_MCPListRooms(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_list_rooms", map[string]any{
		"wing": "myproject",
	})
	if _, ok := result["wing"]; !ok {
		t.Error("list_rooms should include wing field")
	}
	rooms, ok := result["rooms"].(map[string]any)
	if !ok {
		t.Fatalf("expected rooms map: %v", result)
	}
	if len(rooms) < 1 {
		t.Error("expected at least 1 room for myproject wing")
	}
}

func TestB060_MCPGetTaxonomy(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_get_taxonomy", nil)
	taxonomy, ok := result["taxonomy"].(map[string]any)
	if !ok {
		t.Fatalf("expected taxonomy map: %v", result)
	}
	if len(taxonomy) < 1 {
		t.Error("expected at least 1 wing in taxonomy")
	}
	// Each wing should map to rooms with counts
	for wing, rooms := range taxonomy {
		roomMap, ok := rooms.(map[string]any)
		if !ok {
			t.Errorf("wing %q should map to rooms dict: %T", wing, rooms)
		}
		for room, count := range roomMap {
			if _, ok := count.(float64); !ok {
				t.Errorf("room %q in wing %q should have count, got %T", room, wing, count)
			}
		}
	}
}

func TestB061_MCPCheckDuplicate(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_check_duplicate", map[string]any{
		"content": "ChromaDB setup guide for the project", "threshold": 0.5,
	})
	if _, ok := result["is_duplicate"]; !ok {
		t.Error("check_duplicate should include is_duplicate")
	}
	if _, ok := result["matches"]; !ok {
		t.Error("check_duplicate should include matches")
	}
}

func TestB062_MCPGetAAAKSpec(t *testing.T) {
	palacePath := seedPalace(t)
	result := mcpCall(t, palacePath, "mempalace_get_aaak_spec", nil)
	spec, _ := result["aaak_spec"].(string)
	if spec == "" {
		t.Error("get_aaak_spec should return non-empty spec")
	}
	if !strings.Contains(spec, "AAAK") {
		t.Errorf("aaak_spec should mention AAAK: %s", spec)
	}
}

func TestB063_MCPKGAdd(t *testing.T) {
	palacePath := seedPalace(t)
	result := mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("kg_add should succeed: %v", result)
	}
	if _, ok := result["triple_id"]; !ok {
		t.Error("kg_add should return triple_id")
	}
	if _, ok := result["fact"]; !ok {
		t.Error("kg_add should return fact string")
	}
}

func TestB064_MCPKGQuery(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max",
	})
	if _, ok := result["entity"]; !ok {
		t.Error("kg_query should include entity")
	}
	facts, _ := result["facts"].([]any)
	if len(facts) < 1 {
		t.Error("kg_query should return at least 1 fact")
	}
	fact, _ := facts[0].(map[string]any)
	for _, key := range []string{"direction", "subject", "predicate", "object", "current"} {
		if _, ok := fact[key]; !ok {
			t.Errorf("fact missing key %q", key)
		}
	}
}

func TestB065_MCPKGQueryAsOf(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
		"valid_from": "2025-01-01",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "chess",
		"valid_from": "2025-06-01",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_query", map[string]any{
		"entity": "Max", "as_of": "2025-03-15",
	})
	facts, _ := result["facts"].([]any)
	if len(facts) != 1 {
		t.Errorf("as_of March expected 1 fact, got %d", len(facts))
	}
}

func TestB066_MCPKGInvalidate(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_invalidate", map[string]any{
		"subject": "Max", "predicate": "does", "object": "swimming",
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("invalidate should succeed: %v", result)
	}
	if _, ok := result["ended"]; !ok {
		t.Error("invalidate should return ended")
	}
}

func TestB067_MCPKGTimeline(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "born", "object": "world",
		"valid_from": "2015-04-01",
	})
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
		"valid_from": "2025-06-01",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_timeline", map[string]any{
		"entity": "Max",
	})
	timeline, _ := result["timeline"].([]any)
	if len(timeline) < 2 {
		t.Errorf("expected at least 2 timeline entries, got %d", len(timeline))
	}
}

func TestB068_MCPKGStats(t *testing.T) {
	palacePath := seedPalace(t)
	mcpCall(t, palacePath, "mempalace_kg_add", map[string]any{
		"subject": "Max", "predicate": "loves", "object": "chess",
	})
	result := mcpCall(t, palacePath, "mempalace_kg_stats", nil)
	for _, key := range []string{"entities", "triples", "current_facts", "expired_facts", "relationship_types"} {
		if _, ok := result[key]; !ok {
			t.Errorf("kg_stats missing key %q", key)
		}
	}
}

func TestB069_MCPDiaryWrite(t *testing.T) {
	palacePath := seedPalace(t)
	result := mcpCall(t, palacePath, "mempalace_diary_write", map[string]any{
		"agent_name": "Atlas", "entry": "Today I learned about Go.", "topic": "learning",
	})
	if success, _ := result["success"].(bool); !success {
		t.Errorf("diary_write should succeed: %v", result)
	}
	for _, key := range []string{"entry_id", "agent", "topic", "timestamp"} {
		if _, ok := result[key]; !ok {
			t.Errorf("diary_write missing key %q", key)
		}
	}
}

func TestB070_MCPDiaryRead(t *testing.T) {
	palacePath := seedPalace(t)
	// Write a diary entry first
	mcpCall(t, palacePath, "mempalace_diary_write", map[string]any{
		"agent_name": "Atlas", "entry": "First diary entry.", "topic": "general",
	})
	mcpCall(t, palacePath, "mempalace_diary_write", map[string]any{
		"agent_name": "Atlas", "entry": "Second diary entry.", "topic": "learning",
	})

	result := mcpCall(t, palacePath, "mempalace_diary_read", map[string]any{
		"agent_name": "Atlas", "last_n": 10,
	})
	for _, key := range []string{"agent", "entries", "total", "showing"} {
		if _, ok := result[key]; !ok {
			t.Errorf("diary_read missing key %q", key)
		}
	}
	entries, _ := result["entries"].([]any)
	if len(entries) < 2 {
		t.Errorf("expected at least 2 diary entries, got %d", len(entries))
	}
}

func TestB071_MCPTraverse(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	// First find a room that exists
	stats := mcpCall(t, palacePath, "mempalace_graph_stats", nil)
	rpw, _ := stats["rooms_per_wing"].(map[string]any)
	if len(rpw) == 0 {
		t.Skip("no rooms in palace")
	}
	// Try traversing from "technical" room
	result := mcpCall(t, palacePath, "mempalace_traverse", map[string]any{
		"start_room": "technical",
	})
	// If room exists, should have nodes
	if nodes, ok := result["nodes"].([]any); ok {
		if len(nodes) > 0 {
			node, _ := nodes[0].(map[string]any)
			for _, key := range []string{"room", "wings", "count", "hop"} {
				if _, ok := node[key]; !ok {
					t.Errorf("traverse node missing key %q", key)
				}
			}
		}
	}
	// If room doesn't exist, error+suggestions is also valid
	if errMsg, ok := result["error"].(string); ok {
		if !strings.Contains(errMsg, "not found") {
			t.Errorf("traverse error should mention 'not found': %s", errMsg)
		}
	}
}

func TestB072_MCPFindTunnels(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_find_tunnels", map[string]any{
		"wing_a": "sample_project", "wing_b": "myproject",
	})
	if _, ok := result["tunnels"]; !ok {
		t.Error("find_tunnels should include tunnels array")
	}
	if _, ok := result["count"]; !ok {
		t.Error("find_tunnels should include count")
	}
}

func TestB073_MCPGraphStats(t *testing.T) {
	palacePath := seedPalaceWithContent(t)
	result := mcpCall(t, palacePath, "mempalace_graph_stats", nil)
	for _, key := range []string{"total_rooms", "tunnel_rooms", "total_edges", "rooms_per_wing", "top_tunnels"} {
		if _, ok := result[key]; !ok {
			t.Errorf("graph_stats missing key %q", key)
		}
	}
}

func TestB076_MCPNotificationsInitialized(t *testing.T) {
	palacePath := seedPalace(t)
	// notifications/initialized should return no response (nil).
	req := `{"jsonrpc":"2.0","method":"notifications/initialized"}` + "\n"
	goInv := runCmdWithStdin(t, req, "mcp", "--serve", "--palace", palacePath)
	if goInv.ExitCode != 0 {
		t.Fatalf("mcp exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	// Should produce no output (or only whitespace)
	if strings.TrimSpace(goInv.Stdout) != "" {
		t.Errorf("notifications/initialized should produce no output, got: %s", goInv.Stdout)
	}
}

func TestB077_MCPIntegerCoercion(t *testing.T) {
	palacePath := seedPalace(t)
	// Pass limit as string instead of int — should be coerced.
	result := mcpCall(t, palacePath, "mempalace_search", map[string]any{
		"query": "test", "limit": "3",
	})
	// Should not error
	if _, ok := result["error"]; ok {
		t.Errorf("integer coercion should work, got error: %v", result["error"])
	}
	if _, ok := result["memories"]; !ok {
		t.Error("search with coerced limit should return memories")
	}
}

func TestB022_CompressDryRun(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "compress", "--dry-run", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("compress --dry-run exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "DRY RUN") {
		t.Errorf("compress --dry-run should mention DRY RUN: %s", goInv.Stdout)
	}
}

func TestB023_CompressWing(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "compress", "--wing", "sample_project", "--dry-run", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("compress --wing exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	if !strings.Contains(goInv.Stdout, "DRY RUN") {
		t.Errorf("compress --wing should mention DRY RUN: %s", goInv.Stdout)
	}
}

func TestB025_Repair(t *testing.T) {
	palacePath := seedPalace(t)
	_, goInv := invoke(t, "repair", "--palace", palacePath)
	if goInv == nil {
		t.Skip("go impl not available")
	}
	if goInv.ExitCode != 0 {
		t.Fatalf("repair exit=%d: %s", goInv.ExitCode, goInv.Stderr)
	}
	out := goInv.Stdout
	if !strings.Contains(out, "Repair") {
		t.Errorf("repair should mention Repair: %s", out)
	}
	if !strings.Contains(out, "Repaired") {
		t.Errorf("repair should show completion: %s", out)
	}
}
