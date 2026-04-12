package instructions

import (
	"strings"
	"testing"
)

func TestGetAvailable(t *testing.T) {
	for _, name := range Available() {
		text, err := Get(name)
		if err != nil {
			t.Errorf("Get(%q): %v", name, err)
		}
		if text == "" {
			t.Errorf("Get(%q) returned empty", name)
		}
	}
}

func TestGetInit(t *testing.T) {
	text, err := Get("init")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text, "MemPalace Init") {
		t.Error("init instructions missing expected header")
	}
}

func TestGetUnknown(t *testing.T) {
	_, err := Get("nonexistent")
	if err == nil {
		t.Error("expected error for unknown instruction")
	}
}

func TestAvailable(t *testing.T) {
	avail := Available()
	if len(avail) != 5 {
		t.Errorf("expected 5 available instructions, got %d: %v", len(avail), avail)
	}
}
