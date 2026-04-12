package miner

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// File-type + safety constants, ported from mempalace/miner.py:22-57 and
// mempalace/palace.py:9-33.
var (
	readableExtensions = map[string]struct{}{
		".txt": {}, ".md": {}, ".py": {}, ".js": {}, ".ts": {},
		".jsx": {}, ".tsx": {}, ".json": {}, ".yaml": {}, ".yml": {},
		".html": {}, ".css": {}, ".java": {}, ".go": {}, ".rs": {},
		".rb": {}, ".sh": {}, ".csv": {}, ".sql": {}, ".toml": {},
	}
	skipFilenames = map[string]struct{}{
		"mempalace.yaml":    {},
		"mempalace.yml":     {},
		"mempal.yaml":       {},
		"mempal.yml":        {},
		"entities.json":     {},
		".gitignore":        {},
		"package-lock.json": {},
	}
	// skipDirs is the full miner-side blocklist (superset of the one in
	// internal/room). Directory names that match are pruned at WalkDir
	// visit time via filepath.SkipDir.
	skipDirs = map[string]struct{}{
		".git": {}, "node_modules": {}, "__pycache__": {}, ".venv": {},
		"venv": {}, "env": {}, "dist": {}, "build": {}, ".next": {},
		"coverage": {}, ".mempalace": {}, ".ruff_cache": {},
		".mypy_cache": {}, ".pytest_cache": {}, ".cache": {}, ".tox": {},
		".nox": {}, ".idea": {}, ".vscode": {}, ".ipynb_checkpoints": {},
		".eggs": {}, "htmlcov": {}, "target": {},
	}
)

// MaxFileSize is the per-file byte cap applied by the scanner — files
// over the limit are silently skipped (matches miner.py:57).
const MaxFileSize int64 = 10 * 1024 * 1024

// ScanOptions controls ScanProject. The zero value is unusable — pass
// RespectGitignore: true for default behavior.
type ScanOptions struct {
	// RespectGitignore toggles .gitignore processing across the walk.
	RespectGitignore bool
	// IncludeIgnored holds project-relative paths that must never be
	// filtered, even if a .gitignore or skip-dir rule would reject them.
	IncludeIgnored []string
}

// ScanProject returns every readable file under projectDir, sorted
// lexicographically for determinism. Port of scan_project in
// mempalace/miner.py:471-532; the sort is a deliberate Go-side addition
// so tests don't depend on filesystem readdir order.
func ScanProject(projectDir string, opts ScanOptions) ([]string, error) {
	absDir, err := filepath.Abs(projectDir)
	if err != nil {
		return nil, fmt.Errorf("scan: abs %s: %w", projectDir, err)
	}

	includeSet := normalizeIncludePaths(opts.IncludeIgnored)
	matcherCache := map[string]*GitignoreMatcher{}
	var files []string

	walk := func(current string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		// Active matcher chain = every loaded matcher whose base dir is
		// an ancestor of (or equal to) the current directory.
		activeMatchers := func(dir string) []*GitignoreMatcher {
			var out []*GitignoreMatcher
			for base, m := range matcherCache {
				if m == nil {
					continue
				}
				if dir == base || strings.HasPrefix(dir+string(filepath.Separator), base+string(filepath.Separator)) {
					out = append(out, m)
				}
			}
			sort.Slice(out, func(i, j int) bool {
				if len(out[i].baseDir) != len(out[j].baseDir) {
					return len(out[i].baseDir) < len(out[j].baseDir)
				}
				return out[i].baseDir < out[j].baseDir
			})
			return out
		}

		rel, _ := filepath.Rel(absDir, current)
		relPosix := filepath.ToSlash(rel)
		if relPosix == "." {
			relPosix = ""
		}

		if d.IsDir() {
			// Load this directory's .gitignore (if any) BEFORE making
			// any decision about its own skip status — Python applies
			// the matcher to the directory's children, not itself.
			if opts.RespectGitignore {
				if _, seen := matcherCache[current]; !seen {
					m, err := loadGitignoreMatcher(current)
					if err != nil && !os.IsNotExist(err) {
						return err
					}
					matcherCache[current] = m
				}
			}

			// Never prune the walk root itself.
			if current == absDir {
				return nil
			}

			forceInclude := isForceIncluded(relPosix, includeSet)
			if !forceInclude && shouldSkipDir(d.Name()) {
				return filepath.SkipDir
			}
			if opts.RespectGitignore && !forceInclude {
				parent := filepath.Dir(current)
				if isGitignored(current, activeMatchers(parent), true) {
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Files ----------------------------------------------------------
		name := d.Name()
		forceInclude := isForceIncluded(relPosix, includeSet)
		exactForce := isExactForceIncluded(relPosix, includeSet)

		if !forceInclude {
			if _, skip := skipFilenames[name]; skip {
				return nil
			}
		}
		if _, ok := readableExtensions[strings.ToLower(filepath.Ext(name))]; !ok && !exactForce {
			return nil
		}

		parent := filepath.Dir(current)
		if opts.RespectGitignore && !forceInclude {
			if isGitignored(current, activeMatchers(parent), false) {
				return nil
			}
		}

		info, err := d.Info()
		if err != nil {
			return nil
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if info.Size() > MaxFileSize {
			return nil
		}
		files = append(files, current)
		return nil
	}

	if err := filepath.WalkDir(absDir, walk); err != nil {
		return nil, fmt.Errorf("scan: walk: %w", err)
	}

	sort.Strings(files)
	return files, nil
}

// shouldSkipDir returns true for the blocklist names plus any directory
// ending in ".egg-info" — parity with should_skip_dir in miner.py:198.
func shouldSkipDir(name string) bool {
	if _, ok := skipDirs[name]; ok {
		return true
	}
	return strings.HasSuffix(name, ".egg-info")
}

// normalizeIncludePaths mirrors normalize_include_paths in miner.py:203
// — it trims leading/trailing slashes and drops empty strings so the
// set lookup is a single strings.ToSlash away.
func normalizeIncludePaths(raw []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, p := range raw {
		trimmed := strings.Trim(strings.TrimSpace(p), "/")
		if trimmed == "" {
			continue
		}
		out[path.Clean(filepath.ToSlash(trimmed))] = struct{}{}
	}
	return out
}

// isForceIncluded returns true when rel (posix, project-relative) is
// an ancestor of, descendant of, or equal to one of the include entries.
func isForceIncluded(rel string, include map[string]struct{}) bool {
	if len(include) == 0 {
		return false
	}
	if rel == "" {
		return false
	}
	for inc := range include {
		if rel == inc {
			return true
		}
		if strings.HasPrefix(rel, inc+"/") {
			return true
		}
		if strings.HasPrefix(inc, rel+"/") {
			return true
		}
	}
	return false
}

// isExactForceIncluded is the stricter check — only exact matches count,
// used to bypass the readable-extensions filter (Python does this so
// `--include-ignored notes.pdf` actually pulls the pdf in).
func isExactForceIncluded(rel string, include map[string]struct{}) bool {
	if rel == "" {
		return false
	}
	_, ok := include[rel]
	return ok
}
