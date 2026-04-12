// Package config loads MemPalace runtime configuration from environment
// variables, a JSON file at ~/.mempalace/config.json, and compiled-in
// defaults — in that precedence order. Mirrors mempalace/config.py:115-209.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
)

// DefaultCollectionName is the name used for the single drawers collection.
const DefaultCollectionName = "mempalace_drawers"

// DefaultTopicWings mirrors mempalace/config.py:64-72.
var DefaultTopicWings = []string{
	"emotions",
	"consciousness",
	"memory",
	"technical",
	"identity",
	"family",
	"creative",
}

// DefaultHallKeywords mirrors mempalace/config.py:74-112.
var DefaultHallKeywords = map[string][]string{
	"emotions": {
		"scared", "afraid", "worried", "happy", "sad", "love", "hate",
		"feel", "cry", "tears",
	},
	"consciousness": {
		"consciousness", "conscious", "aware", "real", "genuine",
		"soul", "exist", "alive",
	},
	"memory": {
		"memory", "remember", "forget", "recall", "archive", "palace", "store",
	},
	"technical": {
		"code", "python", "script", "bug", "error", "function", "api",
		"database", "server",
	},
	"identity": {
		"identity", "name", "who am i", "persona", "self",
	},
	"family": {
		"family", "kids", "children", "daughter", "son", "parent",
		"mother", "father",
	},
	"creative": {
		"game", "gameplay", "player", "app", "design", "art", "music", "story",
	},
}

// Config holds runtime configuration. Construct via Load.
type Config struct {
	// PalacePath is the filesystem location of the sqlite-vec palace.
	PalacePath string
	// CollectionName is the logical name for drawer storage.
	CollectionName string
	// PeopleMap maps name variants to canonical names (see save_people_map).
	PeopleMap map[string]string
	// TopicWings is the ordered list of topic wing identifiers.
	TopicWings []string
	// HallKeywords maps hall names to keyword lists used for routing.
	HallKeywords map[string][]string

	configDir     string
	configFile    string
	peopleMapFile string
}

// fileConfig is the on-disk JSON shape.
type fileConfig struct {
	PalacePath     string              `json:"palace_path,omitempty"`
	CollectionName string              `json:"collection_name,omitempty"`
	PeopleMap      map[string]string   `json:"people_map,omitempty"`
	TopicWings     []string            `json:"topic_wings,omitempty"`
	HallKeywords   map[string][]string `json:"hall_keywords,omitempty"`
}

// Load reads configuration from configDir (empty = ~/.mempalace), applying
// precedence: environment variables > config.json > defaults. JSON parse
// errors are logged and treated as an empty file. Load never creates files or
// directories — use Init for that.
func Load(configDir string) (*Config, error) {
	dir, err := resolveConfigDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("config: resolve dir: %w", err)
	}

	cfg := &Config{
		configDir:     dir,
		configFile:    filepath.Join(dir, "config.json"),
		peopleMapFile: filepath.Join(dir, "people_map.json"),
	}

	fc := loadFileConfig(cfg.configFile)

	// palace_path: env > file > default.
	// NOTE: the default mirrors Python mempalace/config.py:63 — it is
	// ALWAYS ~/.mempalace/palace, regardless of whether configDir was
	// overridden. This matches the behavioral oracle even when tests pass
	// a custom configDir.
	cfg.PalacePath = envOrDefault(
		"MEMPALACE_PALACE_PATH",
		envOrDefault("MEMPAL_PALACE_PATH",
			stringOrDefault(fc.PalacePath, defaultPalacePath())),
	)

	// collection_name: file > default (no env var in Python oracle)
	cfg.CollectionName = stringOrDefault(fc.CollectionName, DefaultCollectionName)

	// topic_wings: file > default
	if len(fc.TopicWings) > 0 {
		cfg.TopicWings = fc.TopicWings
	} else {
		cfg.TopicWings = append([]string(nil), DefaultTopicWings...)
	}

	// hall_keywords: file > default
	if len(fc.HallKeywords) > 0 {
		cfg.HallKeywords = fc.HallKeywords
	} else {
		cfg.HallKeywords = cloneHallKeywords(DefaultHallKeywords)
	}

	// people_map: separate file > file key > empty
	cfg.PeopleMap = loadPeopleMap(cfg.peopleMapFile, fc.PeopleMap)

	return cfg, nil
}

// Init creates the config directory with 0700 and, if config.json is absent,
// writes it with 0600 containing the current defaults.
func (c *Config) Init() error {
	if err := os.MkdirAll(c.configDir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", c.configDir, err)
	}
	// Ensure perms even if the directory already existed.
	_ = os.Chmod(c.configDir, 0o700)

	if _, err := os.Stat(c.configFile); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("config: stat %s: %w", c.configFile, err)
	}

	defaults := fileConfig{
		PalacePath:     defaultPalacePath(),
		CollectionName: DefaultCollectionName,
		TopicWings:     DefaultTopicWings,
		HallKeywords:   DefaultHallKeywords,
	}
	data, err := json.MarshalIndent(defaults, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal defaults: %w", err)
	}
	if err := os.WriteFile(c.configFile, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", c.configFile, err)
	}
	return nil
}

// SavePeopleMap writes pm to people_map.json with 0600 permissions. The
// config directory is created if needed.
func (c *Config) SavePeopleMap(pm map[string]string) error {
	if err := os.MkdirAll(c.configDir, 0o700); err != nil {
		return fmt.Errorf("config: mkdir %s: %w", c.configDir, err)
	}
	data, err := json.MarshalIndent(pm, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal people_map: %w", err)
	}
	if err := os.WriteFile(c.peopleMapFile, data, 0o600); err != nil {
		return fmt.Errorf("config: write %s: %w", c.peopleMapFile, err)
	}
	return nil
}

// ConfigDir returns the resolved configuration directory.
func (c *Config) ConfigDir() string { return c.configDir }

// ConfigFile returns the resolved config.json path.
func (c *Config) ConfigFile() string { return c.configFile }

// PeopleMapFile returns the resolved people_map.json path.
func (c *Config) PeopleMapFile() string { return c.peopleMapFile }

// --- helpers ---------------------------------------------------------------

func resolveConfigDir(configDir string) (string, error) {
	if configDir != "" {
		return configDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".mempalace"), nil
}

// defaultPalacePath mirrors Python's module constant DEFAULT_PALACE_PATH
// (mempalace/config.py:63). Python hardcodes ~/.mempalace/palace at import
// time and never re-derives it from config_dir — Go matches. If the home
// directory cannot be resolved, we fall back to a relative ".mempalace/palace"
// which still gives deterministic tests without panicking.
func defaultPalacePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".mempalace", "palace")
	}
	return filepath.Join(home, ".mempalace", "palace")
}

func loadFileConfig(path string) fileConfig {
	var fc fileConfig
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			slog.Warn("config.json read failed, using defaults",
				"path", path, "error", err)
		}
		return fc
	}
	if err := json.Unmarshal(data, &fc); err != nil {
		slog.Warn("config.json parse failed, using defaults",
			"path", path, "error", err)
		return fileConfig{}
	}
	return fc
}

func loadPeopleMap(path string, fileFallback map[string]string) map[string]string {
	data, err := os.ReadFile(path)
	if err == nil {
		var pm map[string]string
		if jerr := json.Unmarshal(data, &pm); jerr == nil && pm != nil {
			return pm
		}
		slog.Warn("people_map.json parse failed, falling back",
			"path", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		slog.Warn("people_map.json read failed, falling back",
			"path", path, "error", err)
	}
	if fileFallback != nil {
		return fileFallback
	}
	return map[string]string{}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func stringOrDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}

func cloneHallKeywords(in map[string][]string) map[string][]string {
	out := make(map[string][]string, len(in))
	for k, v := range in {
		out[k] = append([]string(nil), v...)
	}
	return out
}
