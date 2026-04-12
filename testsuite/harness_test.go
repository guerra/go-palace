//go:build testsuite
// +build testsuite

// Package testsuite_test holds the behavioral equivalence suite that drives
// both the Go binary and the Python oracle via subprocess and compares
// observable outputs. Build tag "testsuite" keeps it out of the default
// `go test ./...` pipeline so make audit does not require Python.
package testsuite_test

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
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
	patterns := []string{`(?i)mempalace`, `(?i)init`, `(?i)mine`, `(?i)search`, `(?i)status`}
	compareStructural(t, "B-001", py, goInv, patterns)
	compareExitCode(t, "B-001", py, goInv)
}

func TestB002_HelpFlag(t *testing.T) {
	py, goInv := invoke(t, "--help")
	patterns := []string{`(?i)usage`, `(?i)init`, `(?i)mine`, `(?i)search`, `(?i)status`}
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
