package main

import (
	"bytes"
	"errors"
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
	if !strings.Contains(out, "ok") {
		t.Errorf("expected output to contain 'ok', got %q", out)
	}
}

func TestRootHasExpectedSubcommands(t *testing.T) {
	root := newRootCmd()
	want := map[string]bool{"status": false, "mine": false, "search": false}
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

func TestMineStubReturnsErrNotImplemented(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"mine", "."})
	err := root.Execute()
	if !errors.Is(err, ErrNotImplementedPhaseA) {
		t.Errorf("got %v, want ErrNotImplementedPhaseA", err)
	}
}

func TestSearchStubReturnsErrNotImplemented(t *testing.T) {
	root := newRootCmd()
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	root.SetArgs([]string{"search", "hello"})
	err := root.Execute()
	if !errors.Is(err, ErrNotImplementedPhaseA) {
		t.Errorf("got %v, want ErrNotImplementedPhaseA", err)
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
	for _, want := range []string{"mine", "search", "status"} {
		if !strings.Contains(out, want) {
			t.Errorf("help missing %q: %s", want, out)
		}
	}
}
