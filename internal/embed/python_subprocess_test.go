package embed_test

import (
	"errors"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"go-palace/internal/embed"
)

// fakeScriptPath returns the absolute path to testdata/fake_embedder.py.
// Tests cd is the package dir (internal/embed), so we walk up to the repo
// root to find testdata.
func fakeScriptPath(t *testing.T) string {
	t.Helper()
	abs, err := filepath.Abs(filepath.Join("..", "..", "testdata", "fake_embedder.py"))
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func requirePython(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not in PATH")
	}
}

func TestPythonSubprocessFakeScript(t *testing.T) {
	requirePython(t)
	e, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		PythonArgs: []string{"python3", fakeScriptPath(t)},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	t.Cleanup(func() { _ = e.Close() })

	if got := e.Dimension(); got != 4 {
		t.Errorf("dim: got %d, want 4", got)
	}

	vecs, err := e.Embed([]string{"a", "b"})
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(vecs) != 2 {
		t.Fatalf("len: got %d, want 2", len(vecs))
	}
	for i, v := range vecs {
		if len(v) != 4 {
			t.Errorf("vec[%d] len: got %d, want 4", i, len(v))
		}
	}

	// Determinism: two calls with same input give identical vectors.
	vecs2, err := e.Embed([]string{"a"})
	if err != nil {
		t.Fatalf("embed2: %v", err)
	}
	for i := range vecs[0] {
		if vecs2[0][i] != vecs[0][i] {
			t.Errorf("non-deterministic at %d: %f vs %f",
				i, vecs2[0][i], vecs[0][i])
		}
	}
}

func TestPythonSubprocessClose(t *testing.T) {
	requirePython(t)
	e, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		PythonArgs: []string{"python3", fakeScriptPath(t)},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Errorf("first close: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Errorf("second close: %v", err)
	}
}

func TestPythonSubprocessEmbedAfterClose(t *testing.T) {
	requirePython(t)
	e, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		PythonArgs: []string{"python3", fakeScriptPath(t)},
	})
	if err != nil {
		t.Fatalf("new: %v", err)
	}
	_ = e.Close()
	if _, err := e.Embed([]string{"a"}); !errors.Is(err, embed.ErrClosed) {
		t.Errorf("expected ErrClosed, got %v", err)
	}
}

func TestPythonSubprocessProbeFailure(t *testing.T) {
	// /bin/false exits immediately with no output — probe must fail.
	_, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		PythonArgs: []string{"/bin/false"},
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, embed.ErrProbeFailed) {
		t.Errorf("expected ErrProbeFailed, got %v", err)
	}
}

func TestPythonSubprocessMissingBinary(t *testing.T) {
	_, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		PythonArgs: []string{"/nonexistent/embed-xyz-does-not-exist"},
	})
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	if !strings.Contains(err.Error(), "start") && !errors.Is(err, embed.ErrProbeFailed) {
		t.Errorf("err = %v, want start or probe failure", err)
	}
}

func TestPythonSubprocessMempalaceDirUnset(t *testing.T) {
	// No PythonArgs override, no MempalaceDir, no env var — must fail fast
	// with ErrMempalaceDirUnset and NOT try to spawn anything.
	t.Setenv(embed.MempalaceDirEnv, "")
	_, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{})
	if !errors.Is(err, embed.ErrMempalaceDirUnset) {
		t.Errorf("got %v, want ErrMempalaceDirUnset", err)
	}
}

func TestPythonSubprocessMempalaceDirEnv(t *testing.T) {
	// Env var honored: an obviously broken dir should reach the start/probe
	// path (which fails), proving the env was consulted.
	t.Setenv(embed.MempalaceDirEnv, "/nonexistent/mempalace-xyz")
	_, err := embed.NewPythonSubprocessEmbedder(embed.PythonSubprocessOptions{
		UVBinary: "/nonexistent/uv-xyz-does-not-exist",
	})
	if err == nil {
		t.Fatal("expected failure from broken uv path")
	}
	if errors.Is(err, embed.ErrMempalaceDirUnset) {
		t.Errorf("env var was ignored: got %v", err)
	}
}
