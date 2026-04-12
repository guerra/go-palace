package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeFile(t *testing.T, path, data string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestLoadDefaults(t *testing.T) {
	dir := t.TempDir()
	// Clear env so we see the pure default.
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("home: %v", err)
	}
	if got, want := cfg.PalacePath, filepath.Join(home, ".mempalace", "palace"); got != want {
		t.Errorf("palace path: got %q want %q", got, want)
	}
	if cfg.CollectionName != DefaultCollectionName {
		t.Errorf("collection name: got %q want %q", cfg.CollectionName, DefaultCollectionName)
	}
	if len(cfg.TopicWings) != len(DefaultTopicWings) {
		t.Errorf("topic wings len: got %d want %d", len(cfg.TopicWings), len(DefaultTopicWings))
	}
	if len(cfg.HallKeywords) == 0 {
		t.Errorf("hall keywords empty")
	}
	if cfg.PeopleMap == nil {
		t.Errorf("people map nil (should be empty map)")
	}
}

func TestEnvOverride(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_PALACE_PATH", "/tmp/envx")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PalacePath != "/tmp/envx" {
		t.Errorf("got %q, want /tmp/envx", cfg.PalacePath)
	}
}

func TestEnvFallbackMempal(t *testing.T) {
	dir := t.TempDir()
	// Explicitly clear the primary env so the fallback triggers.
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "/tmp/mempal-y")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PalacePath != "/tmp/mempal-y" {
		t.Errorf("got %q, want /tmp/mempal-y", cfg.PalacePath)
	}
}

func TestFileOverride(t *testing.T) {
	dir := t.TempDir()
	// Ensure env does not interfere.
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"palace_path":"/tmp/zfile","collection_name":"alt"}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PalacePath != "/tmp/zfile" {
		t.Errorf("palace: got %q, want /tmp/zfile", cfg.PalacePath)
	}
	if cfg.CollectionName != "alt" {
		t.Errorf("collection: got %q, want alt", cfg.CollectionName)
	}
}

func TestCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	writeFile(t, filepath.Join(dir, "config.json"), `{bad json`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load should swallow parse errors: %v", err)
	}
	// Should fall back to the Python-parity default (~/.mempalace/palace).
	home, herr := os.UserHomeDir()
	if herr != nil {
		t.Fatalf("home: %v", herr)
	}
	if got, want := cfg.PalacePath, filepath.Join(home, ".mempalace", "palace"); got != want {
		t.Errorf("palace: got %q, want %q", got, want)
	}
}

func TestPeopleMapSeparateFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "people_map.json"),
		`{"gabe":"Gabriel","ga":"Gabriel"}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PeopleMap["gabe"] != "Gabriel" {
		t.Errorf("people map: missing gabe entry, got %+v", cfg.PeopleMap)
	}
}

func TestPeopleMapFallbackToConfigKey(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"people_map":{"alias":"Real"}}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PeopleMap["alias"] != "Real" {
		t.Errorf("people map: expected alias->Real, got %+v", cfg.PeopleMap)
	}
}

func TestPeopleMapSeparateWins(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"),
		`{"people_map":{"a":"FromConfig"}}`)
	writeFile(t, filepath.Join(dir, "people_map.json"),
		`{"a":"FromSeparate"}`)
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PeopleMap["a"] != "FromSeparate" {
		t.Errorf("people map: expected separate file to win, got %+v", cfg.PeopleMap)
	}
}

func TestInitCreatesFiles(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", ".mempalace")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if err := cfg.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}

	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("%s is not a directory", dir)
	}

	cfgPath := filepath.Join(dir, "config.json")
	cfgInfo, err := os.Stat(cfgPath)
	if err != nil {
		t.Fatalf("stat config.json: %v", err)
	}

	if runtime.GOOS != "windows" {
		if perm := info.Mode().Perm(); perm != 0o700 {
			t.Errorf("dir perm: got %o, want 700", perm)
		}
		if perm := cfgInfo.Mode().Perm(); perm != 0o600 {
			t.Errorf("config perm: got %o, want 600", perm)
		}
	}

	// Config content must parse and contain defaults.
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var fc fileConfig
	if err := json.Unmarshal(data, &fc); err != nil {
		t.Fatalf("parse written config: %v", err)
	}
	if fc.CollectionName != DefaultCollectionName {
		t.Errorf("written collection: got %q want %q", fc.CollectionName, DefaultCollectionName)
	}
}

func TestInitIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// Pre-populate a custom config.json.
	writeFile(t, filepath.Join(dir, "config.json"), `{"collection_name":"kept"}`)
	if err := cfg.Init(); err != nil {
		t.Fatalf("init: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(data) != `{"collection_name":"kept"}` {
		t.Errorf("init overwrote existing config.json: %s", data)
	}
}

func TestSavePeopleMap(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	pm := map[string]string{"gabe": "Gabriel"}
	if err := cfg.SavePeopleMap(pm); err != nil {
		t.Fatalf("save: %v", err)
	}
	reread, err := Load(dir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reread.PeopleMap["gabe"] != "Gabriel" {
		t.Errorf("reload missing entry: %+v", reread.PeopleMap)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(filepath.Join(dir, "people_map.json"))
		if err != nil {
			t.Fatalf("stat: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Errorf("people_map perm: got %o want 600", perm)
		}
	}
}

func TestEmptyEnvVarDoesNotOverride(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "config.json"), `{"palace_path":"/tmp/file-wins"}`)
	t.Setenv("MEMPALACE_PALACE_PATH", "")
	t.Setenv("MEMPAL_PALACE_PATH", "")
	cfg, err := Load(dir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if cfg.PalacePath != "/tmp/file-wins" {
		t.Errorf("got %q, want /tmp/file-wins (empty env should not override file)", cfg.PalacePath)
	}
}
