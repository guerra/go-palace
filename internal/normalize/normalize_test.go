package normalize

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeReadsPlainText(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(path, []byte("hello world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if got != "hello world\n" {
		t.Errorf("got %q, want %q", got, "hello world\n")
	}
}

func TestNormalizePreservesInvalidUTF8(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.bin")
	// 0xff,0xfe is invalid as standalone UTF-8; Python errors="replace"
	// would emit U+FFFD but a byte-level round-trip is enough for Phase B.
	if err := os.WriteFile(path, []byte{0xff, 0xfe, 'a'}, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Normalize(path)
	if err != nil {
		t.Fatalf("Normalize: %v", err)
	}
	if len(got) != 3 {
		t.Errorf("got len %d, want 3", len(got))
	}
}

func TestNormalizeRejectsHugeFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "huge.txt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	// Use Truncate to make a sparse file > MaxFileSize without actually
	// writing 500 MB of data.
	if err := f.Truncate(MaxFileSize + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_ = f.Close()

	_, err = Normalize(path)
	if err == nil {
		t.Fatal("expected error on oversized file")
	}
	if !strings.Contains(err.Error(), "too large") {
		t.Errorf("error %v missing 'too large'", err)
	}
}

func TestNormalizeMissingFile(t *testing.T) {
	_, err := Normalize(filepath.Join(t.TempDir(), "missing.txt"))
	if err == nil {
		t.Fatal("expected error on missing file")
	}
	if !strings.Contains(err.Error(), "stat") {
		t.Errorf("error %v missing 'stat'", err)
	}
}
