// Package room implements project-local room detection and the
// mempalace.yaml persistence format. Ports mempalace/room_detector_local.py
// minus the interactive approval flow (Phase B init uses --yes or a
// press-enter shim).
package room

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"
)

// Room is one logical bucket within a wing (project). Matches the yaml
// schema written by mempalace/room_detector_local.py:255.
type Room struct {
	Name        string   `yaml:"name"`
	Description string   `yaml:"description"`
	Keywords    []string `yaml:"keywords"`
}

// Config is the full mempalace.yaml shape consumed by miner + init.
type Config struct {
	Wing  string `yaml:"wing"`
	Rooms []Room `yaml:"rooms"`
}

// ConfigFilename is the canonical mempalace config filename. A legacy
// fallback ("mempal.yaml") is tried by LoadConfig for pre-rename projects.
const ConfigFilename = "mempalace.yaml"

// LegacyConfigFilename is the pre-rename filename. LoadConfig falls back
// here when ConfigFilename is missing — mirrors mempalace/miner.py:259-268.
const LegacyConfigFilename = "mempal.yaml"

// Detect walks dir's top-level (and one level deeper) looking for folder
// names that map into folderRoomMap. If no signal is found it falls back
// to filename-pattern detection. The "general" room is always appended
// last when not already present. Ports detect_rooms_from_folders and
// detect_rooms_from_files from room_detector_local.py.
func Detect(dir string) ([]Room, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("room: abs: %w", err)
	}

	foundOrder := []string{}     // preserves insertion order
	found := map[string]string{} // room_name → original folder name

	addIfNew := func(roomName, original string) {
		if _, ok := found[roomName]; ok {
			return
		}
		found[roomName] = original
		foundOrder = append(foundOrder, roomName)
	}

	top, err := listSortedDirs(absDir)
	if err != nil {
		return nil, fmt.Errorf("room: readdir %s: %w", absDir, err)
	}
	for _, name := range top {
		if _, skip := skipDirs[name]; skip {
			continue
		}
		lower := strings.ReplaceAll(strings.ToLower(name), "-", "_")
		if mapped, ok := folderRoomMap[lower]; ok {
			addIfNew(mapped, name)
			continue
		}
		if len(name) > 2 && isLetter(rune(name[0])) {
			clean := strings.ReplaceAll(strings.ReplaceAll(strings.ToLower(name), "-", "_"), " ", "_")
			addIfNew(clean, name)
		}
	}

	// One level deeper — only top-level dirs in folderRoomMap are checked
	// a second time at depth 2, matching Python's behavior.
	for _, parent := range top {
		if _, skip := skipDirs[parent]; skip {
			continue
		}
		children, err := listSortedDirs(filepath.Join(absDir, parent))
		if err != nil {
			// Silently ignore unreadable subdirs — parity with Python's
			// iterdir which simply doesn't yield them.
			continue
		}
		for _, sub := range children {
			if _, skip := skipDirs[sub]; skip {
				continue
			}
			lower := strings.ReplaceAll(strings.ToLower(sub), "-", "_")
			if mapped, ok := folderRoomMap[lower]; ok {
				addIfNew(mapped, sub)
			}
		}
	}

	rooms := make([]Room, 0, len(foundOrder)+1)
	for _, roomName := range foundOrder {
		original := found[roomName]
		rooms = append(rooms, Room{
			Name:        roomName,
			Description: fmt.Sprintf("Files from %s/", original),
			Keywords:    []string{roomName, strings.ToLower(original)},
		})
	}

	// If folder-signal is weak (≤ 1 non-general room) try filename fallback.
	if len(rooms) <= 1 {
		filenameRooms, err := detectFromFiles(absDir)
		if err == nil && len(filenameRooms) > 0 {
			rooms = filenameRooms
		}
	}

	if !hasRoom(rooms, "general") {
		rooms = append(rooms, Room{
			Name:        "general",
			Description: "Files that don't fit other rooms",
			Keywords:    nil,
		})
	}
	return rooms, nil
}

// detectFromFiles counts folderRoomMap keyword occurrences in every
// filename under dir, then emits rooms with count ≥ 2 (capped at 6).
// Ports detect_rooms_from_files from room_detector_local.py:168-203.
func detectFromFiles(dir string) ([]Room, error) {
	counts := map[string]int{}
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if _, skip := skipDirs[d.Name()]; skip && path != dir {
				return filepath.SkipDir
			}
			return nil
		}
		lower := strings.ReplaceAll(
			strings.ReplaceAll(strings.ToLower(d.Name()), "-", "_"),
			" ", "_",
		)
		for keyword, roomName := range folderRoomMap {
			if strings.Contains(lower, keyword) {
				counts[roomName]++
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	type kv struct {
		name  string
		count int
	}
	pairs := make([]kv, 0, len(counts))
	for name, count := range counts {
		pairs = append(pairs, kv{name, count})
	}
	sort.Slice(pairs, func(i, j int) bool {
		if pairs[i].count != pairs[j].count {
			return pairs[i].count > pairs[j].count
		}
		return pairs[i].name < pairs[j].name
	})

	rooms := []Room{}
	for _, p := range pairs {
		if p.count < 2 {
			continue
		}
		rooms = append(rooms, Room{
			Name:        p.name,
			Description: fmt.Sprintf("Files related to %s", p.name),
			Keywords:    []string{p.name},
		})
		if len(rooms) >= 6 {
			break
		}
	}
	return rooms, nil
}

// SaveConfig serialises {wing, rooms} to dir/mempalace.yaml. Empty keyword
// lists are replaced with [name] so every room has at least one keyword,
// matching save_config in room_detector_local.py:255-274.
func SaveConfig(dir, wing string, rooms []Room) error {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("room: save abs: %w", err)
	}
	out := make([]Room, len(rooms))
	for i, r := range rooms {
		out[i] = r
		if len(out[i].Keywords) == 0 {
			out[i].Keywords = []string{r.Name}
		}
	}
	data, err := yaml.Marshal(Config{Wing: wing, Rooms: out})
	if err != nil {
		return fmt.Errorf("room: marshal yaml: %w", err)
	}
	path := filepath.Join(absDir, ConfigFilename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("room: write %s: %w", path, err)
	}
	return nil
}

// LoadConfig reads dir/mempalace.yaml (falling back to mempal.yaml) and
// returns (wing, rooms). Missing files return a non-nil error so the
// caller can tell the user to run `mempalace init`.
func LoadConfig(dir string) (string, []Room, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", nil, fmt.Errorf("room: load abs: %w", err)
	}
	primary := filepath.Join(absDir, ConfigFilename)
	legacy := filepath.Join(absDir, LegacyConfigFilename)

	var data []byte
	data, err = os.ReadFile(primary)
	if err != nil {
		if !os.IsNotExist(err) {
			return "", nil, fmt.Errorf("room: read %s: %w", primary, err)
		}
		data, err = os.ReadFile(legacy)
		if err != nil {
			return "", nil, fmt.Errorf("room: no mempalace.yaml in %s: run `mempalace init %s`",
				absDir, dir)
		}
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return "", nil, fmt.Errorf("room: parse yaml: %w", err)
	}
	return cfg.Wing, cfg.Rooms, nil
}

// listSortedDirs returns every directory entry beneath dir, sorted
// lexicographically, so Detect's output is stable across filesystems.
func listSortedDirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

func hasRoom(rooms []Room, name string) bool {
	for _, r := range rooms {
		if r.Name == name {
			return true
		}
	}
	return false
}

func isLetter(r rune) bool {
	return unicode.IsLetter(r)
}
