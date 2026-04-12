package room

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasRoomName(rooms []Room, name string) bool {
	for _, r := range rooms {
		if r.Name == name {
			return true
		}
	}
	return false
}

func TestDetectFromFolders(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "frontend"))
	mkdir(t, filepath.Join(dir, "backend"))
	mkdir(t, filepath.Join(dir, "docs"))
	mkdir(t, filepath.Join(dir, "node_modules")) // should be skipped
	mkdir(t, filepath.Join(dir, "random"))       // custom room

	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}

	for _, want := range []string{"frontend", "backend", "documentation", "random", "general"} {
		if !hasRoomName(rooms, want) {
			t.Errorf("missing room %q in %+v", want, rooms)
		}
	}
}

func TestDetectAlwaysHasGeneral(t *testing.T) {
	dir := t.TempDir()
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasRoomName(rooms, "general") {
		t.Errorf("general room missing: %+v", rooms)
	}
}

func TestDetectNestedSecondLevel(t *testing.T) {
	dir := t.TempDir()
	// Custom top-level dir; nested "api" should still promote backend.
	mkdir(t, filepath.Join(dir, "src", "api"))
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasRoomName(rooms, "backend") {
		t.Errorf("nested api → backend not detected: %+v", rooms)
	}
}

func TestDetectFilenameFallback(t *testing.T) {
	dir := t.TempDir()
	// No recognised top-level folder → fallback to filename scan.
	write(t, filepath.Join(dir, "api_handler.py"), "x")
	write(t, filepath.Join(dir, "api_route.py"), "x")
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	// Depending on fallback thresholds, general should still be present.
	if !hasRoomName(rooms, "general") {
		t.Errorf("general missing in filename-fallback: %+v", rooms)
	}
}

func TestSaveAndLoadConfigRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rooms := []Room{
		{Name: "frontend", Description: "Files from frontend/", Keywords: []string{"frontend", "ui"}},
		{Name: "general", Description: "Files that don't fit other rooms", Keywords: nil},
	}
	if err := SaveConfig(dir, "myproject", rooms); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, ConfigFilename))
	if err != nil {
		t.Fatalf("read yaml: %v", err)
	}
	yamlStr := string(data)
	if !strings.Contains(yamlStr, "wing: myproject") {
		t.Errorf("yaml missing wing: %s", yamlStr)
	}
	if !strings.Contains(yamlStr, "name: frontend") {
		t.Errorf("yaml missing room name: %s", yamlStr)
	}

	wing, gotRooms, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if wing != "myproject" {
		t.Errorf("wing = %q, want myproject", wing)
	}
	if len(gotRooms) != 2 {
		t.Errorf("rooms len = %d, want 2", len(gotRooms))
	}
	if gotRooms[0].Name != "frontend" {
		t.Errorf("rooms[0].Name = %q, want frontend", gotRooms[0].Name)
	}
	// Empty-keyword general room should have been filled with [general].
	if len(gotRooms[1].Keywords) != 1 || gotRooms[1].Keywords[0] != "general" {
		t.Errorf("general keywords = %v, want [general]", gotRooms[1].Keywords)
	}
}

func TestLoadConfigMissingYaml(t *testing.T) {
	dir := t.TempDir()
	_, _, err := LoadConfig(dir)
	if err == nil {
		t.Fatal("expected error for missing yaml")
	}
	if !strings.Contains(err.Error(), "mempalace init") {
		t.Errorf("error %v missing init hint", err)
	}
}

func TestLoadConfigLegacyFallback(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, LegacyConfigFilename),
		"wing: legacy_wing\nrooms:\n  - name: general\n    description: x\n    keywords: []\n")
	wing, rooms, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig legacy: %v", err)
	}
	if wing != "legacy_wing" {
		t.Errorf("wing = %q", wing)
	}
	if len(rooms) != 1 || rooms[0].Name != "general" {
		t.Errorf("rooms = %+v", rooms)
	}
}
