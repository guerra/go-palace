package room

import (
	"fmt"
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

func TestDetectSkipsGitAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, ".git"))
	mkdir(t, filepath.Join(dir, "node_modules"))
	mkdir(t, filepath.Join(dir, "frontend"))
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	for _, r := range rooms {
		if r.Name == ".git" || r.Name == "node_modules" {
			t.Errorf("skip dir %q should not appear as room", r.Name)
		}
	}
}

func TestDetectRoomHasDescription(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "docs"))
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var docRoom *Room
	for _, r := range rooms {
		if r.Name == "documentation" {
			docRoom = &r
			break
		}
	}
	if docRoom == nil {
		t.Fatal("documentation room not found")
	}
	if !strings.Contains(docRoom.Description, "docs") {
		t.Errorf("description should mention 'docs', got %q", docRoom.Description)
	}
}

func TestDetectRoomHasKeywords(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "frontend"))
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	var feRoom *Room
	for _, r := range rooms {
		if r.Name == "frontend" {
			feRoom = &r
			break
		}
	}
	if feRoom == nil {
		t.Fatal("frontend room not found")
	}
	if len(feRoom.Keywords) == 0 {
		t.Error("frontend room should have keywords")
	}
}

func TestDetectCustomNamedDirs(t *testing.T) {
	dir := t.TempDir()
	mkdir(t, filepath.Join(dir, "mylib"))
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasRoomName(rooms, "mylib") {
		t.Errorf("custom dir 'mylib' should appear as room: %+v", rooms)
	}
}

func TestDetectFromFilesMatchingFilenames(t *testing.T) {
	dir := t.TempDir()
	// Create files whose names contain room keywords (no recognised folders).
	for _, name := range []string{"test_auth.py", "test_login.py", "test_api.py"} {
		write(t, filepath.Join(dir, name), "content")
	}
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if !hasRoomName(rooms, "general") {
		t.Errorf("general room should always be present: %+v", rooms)
	}
}

func TestDetectFromFilesEmptyDir(t *testing.T) {
	dir := t.TempDir()
	rooms, err := Detect(dir)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	if len(rooms) < 1 {
		t.Error("should have at least general room")
	}
	if !hasRoomName(rooms, "general") {
		t.Errorf("general missing: %+v", rooms)
	}
}

func TestDetectFromFilesCapsAtSix(t *testing.T) {
	dir := t.TempDir()
	// Create many files with different keywords.
	for _, keyword := range []string{"test", "doc", "api", "config", "frontend", "backend", "design", "meeting"} {
		for i := 0; i < 3; i++ {
			write(t, filepath.Join(dir, fmt.Sprintf("%s_file_%d.txt", keyword, i)), "content")
		}
	}
	// Use detectFromFiles directly.
	rooms, err := detectFromFiles(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(rooms) > 6 {
		t.Errorf("expected <= 6 rooms from files, got %d", len(rooms))
	}
}

func TestFolderRoomMap_ExpectedMappings(t *testing.T) {
	tests := []struct {
		key, want string
	}{
		{"frontend", "frontend"},
		{"backend", "backend"},
		{"docs", "documentation"},
		{"tests", "testing"},
		{"config", "configuration"},
	}
	for _, tt := range tests {
		got, ok := folderRoomMap[tt.key]
		if !ok {
			t.Errorf("folderRoomMap missing key %q", tt.key)
		} else if got != tt.want {
			t.Errorf("folderRoomMap[%q] = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestFolderRoomMap_AlternativeNames(t *testing.T) {
	tests := []struct {
		key, want string
	}{
		{"front_end", "frontend"},
		{"back_end", "backend"},
		{"server", "backend"},
		{"client", "frontend"},
		{"api", "backend"},
	}
	for _, tt := range tests {
		got, ok := folderRoomMap[tt.key]
		if !ok {
			t.Errorf("folderRoomMap missing key %q", tt.key)
		} else if got != tt.want {
			t.Errorf("folderRoomMap[%q] = %q, want %q", tt.key, got, tt.want)
		}
	}
}

func TestSaveConfig_ValidYAML(t *testing.T) {
	dir := t.TempDir()
	rooms := []Room{{Name: "general", Description: "All files", Keywords: nil}}
	if err := SaveConfig(dir, "test_proj", rooms); err != nil {
		t.Fatal(err)
	}
	wing, gotRooms, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if wing != "test_proj" {
		t.Errorf("wing = %q, want test_proj", wing)
	}
	if len(gotRooms) != 1 || gotRooms[0].Name != "general" {
		t.Errorf("rooms = %+v", gotRooms)
	}
}

func TestSaveConfig_EmptyKeywordsFilled(t *testing.T) {
	dir := t.TempDir()
	rooms := []Room{
		{Name: "backend", Description: "Server files", Keywords: nil},
	}
	if err := SaveConfig(dir, "proj", rooms); err != nil {
		t.Fatal(err)
	}
	_, gotRooms, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotRooms[0].Keywords) == 0 {
		t.Error("empty keywords should be filled with [name]")
	}
	if gotRooms[0].Keywords[0] != "backend" {
		t.Errorf("keywords[0] = %q, want backend", gotRooms[0].Keywords[0])
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
