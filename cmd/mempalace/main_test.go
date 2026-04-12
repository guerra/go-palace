package main

import (
	"bytes"
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

func TestRootHasStatusSubcommand(t *testing.T) {
	root := newRootCmd()
	found := false
	for _, c := range root.Commands() {
		if c.Name() == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Error("status subcommand not registered")
	}
}
