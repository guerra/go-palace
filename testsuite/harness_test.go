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
	"os"
	"os/exec"
	"regexp"
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

// TestB001_NoArgs_PrintsHelp checks that running the binary with no args
// prints cobra-style help listing the main subcommands. Phase A narrows the
// pattern set to the subcommands that actually exist today; Phase B will
// widen it.
func TestB001_NoArgs_PrintsHelp(t *testing.T) {
	py, goInv := invoke(t)
	patterns := []string{`(?i)mempalace`, `(?i)mine`, `(?i)search`, `(?i)status`}
	compareStructural(t, "B-001", py, goInv, patterns)
	compareExitCode(t, "B-001", py, goInv)
}

// TestB002_HelpFlag checks that --help prints the usage banner.
func TestB002_HelpFlag(t *testing.T) {
	py, goInv := invoke(t, "--help")
	patterns := []string{`(?i)usage`, `(?i)mine`, `(?i)search`, `(?i)status`}
	compareStructural(t, "B-002", py, goInv, patterns)
	compareExitCode(t, "B-002", py, goInv)
}
