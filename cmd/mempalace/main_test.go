package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStatusCommand(t *testing.T) {
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetArgs([]string{"status"})

	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "mempalace") {
		t.Errorf("expected output to contain 'mempalace', got %q", out)
	}
	// Enhanced status prints version + palace info (or "not found" hint).
	if !strings.Contains(out, "0.1.0") {
		t.Errorf("expected output to contain version, got %q", out)
	}
}

func TestRootHasExpectedSubcommands(t *testing.T) {
	root := newRootCmd()
	want := map[string]bool{"status": false, "init": false, "mine": false, "search": false, "wake-up": false, "split": false, "hook": false, "instructions": false, "repair": false, "compress": false, "mcp": false}
	for _, c := range root.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("subcommand %q not registered", name)
		}
	}
}

func TestSearchEmptyPalace(t *testing.T) {
	palacePath := filepath.Join(t.TempDir(), "empty.db")
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"search", "hello", "--palace", palacePath})
	err := root.Execute()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "No results found") {
		t.Errorf("expected 'No results found' in output: %s", out)
	}
}

func TestRootHasWakeUpSubcommand(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "wake-up" {
			found = true
			break
		}
	}
	if !found {
		t.Error("wake-up subcommand not registered")
	}
}

func TestHelpLists(t *testing.T) {
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--help"})
	if err := root.Execute(); err != nil {
		t.Fatalf("help: %v", err)
	}
	out := buf.String()
	for _, want := range []string{"init", "mine", "search", "status", "wake-up", "split", "hook", "instructions", "repair", "compress", "mcp"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q: %s", want, out)
		}
	}
}

// seedSampleProject writes a tiny project tree into a fresh temp dir so
// init + mine exercise real folder detection without touching the
// committed testdata fixtures.
func seedSampleProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, body string) {
		path := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("frontend/app.js", strings.Repeat("// js line\n", 20))
	write("backend/api.py", strings.Repeat("# py line\n", 20))
	write("docs/readme.md", strings.Repeat("doc line\n", 20))
	return dir
}

func TestInitYesWritesYaml(t *testing.T) {
	dir := seedSampleProject(t)
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"init", dir, "--yes"})
	if err := root.Execute(); err != nil {
		t.Fatalf("init: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"WING:", "ROOM:", "mempalace.yaml"} {
		if !strings.Contains(out, want) {
			t.Errorf("init output missing %q: %s", want, out)
		}
	}

	yamlPath := filepath.Join(dir, "mempalace.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		t.Fatalf("yaml not written: %v", err)
	}
	if !strings.Contains(string(data), "wing:") {
		t.Errorf("yaml missing wing key: %s", data)
	}
}

func TestMineDryRunFixture(t *testing.T) {
	dir := seedSampleProject(t)

	// Pre-seed mempalace.yaml (init not required for this test).
	yaml := "wing: testwing\nrooms:\n  - name: general\n    description: all\n    keywords: []\n"
	if err := os.WriteFile(filepath.Join(dir, "mempalace.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	// --palace /nonexistent proves dry-run never opens the palace.
	root.SetArgs([]string{"mine", dir, "--dry-run", "--palace", "/nonexistent/palace"})
	if err := root.Execute(); err != nil {
		t.Fatalf("mine dry-run: %v", err)
	}

	out := buf.String()
	for _, want := range []string{"DRY RUN", "[DRY RUN]", "MemPalace Mine"} {
		if !strings.Contains(out, want) {
			t.Errorf("mine dry-run output missing %q: %s", want, out)
		}
	}
}

func TestMineRequiresYaml(t *testing.T) {
	dir := t.TempDir()
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"mine", dir, "--dry-run", "--palace", "/nonexistent/palace"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for missing mempalace.yaml")
	}
	if !strings.Contains(err.Error(), "mempalace init") {
		t.Errorf("error %v missing 'mempalace init' hint", err)
	}
}

func TestCompressDryRun(t *testing.T) {
	palacePath := filepath.Join(t.TempDir(), "compress_test.db")
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"compress", "--dry-run", "--palace", palacePath})
	err := root.Execute()
	if err != nil {
		t.Fatalf("compress dry-run: %v", err)
	}
	out := buf.String()
	// Either "No palace" or "DRY RUN" with 0 drawers — both are valid
	if !strings.Contains(out, "DRY RUN") && !strings.Contains(out, "No palace") {
		t.Errorf("expected DRY RUN or No palace in output, got %q", out)
	}
}

func TestCompressWithWingFilter(t *testing.T) {
	palacePath := filepath.Join(t.TempDir(), "compress_wing.db")
	root := newRootCmd()
	buf := &bytes.Buffer{}
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"compress", "--dry-run", "--wing", "conversations", "--palace", palacePath})
	err := root.Execute()
	if err != nil {
		t.Fatalf("compress with wing filter: %v", err)
	}
	// Should complete without error
}
