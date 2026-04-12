package miner

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// gitignoreRule is one parsed entry from a .gitignore file. Matches the
// dict shape in mempalace/miner.py:109-116.
type gitignoreRule struct {
	pattern  string
	anchored bool
	dirOnly  bool
	negated  bool
}

// GitignoreMatcher holds the rules loaded from one directory's .gitignore
// file. Multiple matchers are chained by the scanner — last-match-wins
// across the chain, matching git's documented semantics. Ported from
// mempalace/miner.py:65-178.
type GitignoreMatcher struct {
	baseDir string // absolute
	rules   []gitignoreRule
}

// loadGitignoreMatcher reads dir/.gitignore if present. Returns nil (not
// error) when the file is absent or contains no rules — mirrors the
// classmethod from_dir in the Python source.
func loadGitignoreMatcher(dir string) (*GitignoreMatcher, error) {
	path := filepath.Join(dir, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return parseGitignore(dir, string(data)), nil
}

// parseGitignore parses the byte contents of a .gitignore file against
// the Python rule grammar line-by-line. Used directly by tests to avoid
// touching the filesystem for every case.
func parseGitignore(baseDir, body string) *GitignoreMatcher {
	var rules []gitignoreRule
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
			line = line[1:]
		} else if strings.HasPrefix(line, "#") {
			continue
		}

		negated := strings.HasPrefix(line, "!")
		if negated {
			line = line[1:]
		}
		anchored := strings.HasPrefix(line, "/")
		if anchored {
			line = strings.TrimLeft(line, "/")
		}
		dirOnly := strings.HasSuffix(line, "/")
		if dirOnly {
			line = strings.TrimRight(line, "/")
		}
		if line == "" {
			continue
		}
		rules = append(rules, gitignoreRule{
			pattern: line, anchored: anchored, dirOnly: dirOnly, negated: negated,
		})
	}
	if len(rules) == 0 {
		return nil
	}
	return &GitignoreMatcher{baseDir: baseDir, rules: rules}
}

// Matches walks the rule list and returns the last matching rule's
// decision (true = ignored, false = explicitly kept). Returns a nil
// pointer when no rule matched — callers use that to inherit the parent
// matcher's decision.
func (m *GitignoreMatcher) Matches(target string, isDir bool) *bool {
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil
	}
	rel, err := filepath.Rel(m.baseDir, absTarget)
	if err != nil {
		return nil
	}
	rel = filepath.ToSlash(rel)
	rel = strings.Trim(rel, "/")
	if rel == "" || strings.HasPrefix(rel, "..") {
		return nil
	}

	var decision *bool
	for _, r := range m.rules {
		if m.ruleMatches(r, rel, isDir) {
			val := !r.negated
			decision = &val
		}
	}
	return decision
}

func (m *GitignoreMatcher) ruleMatches(r gitignoreRule, relative string, isDir bool) bool {
	parts := strings.Split(relative, "/")
	patternParts := strings.Split(r.pattern, "/")

	if r.dirOnly {
		target := parts
		if !isDir {
			target = parts[:len(parts)-1]
		}
		if len(target) == 0 {
			return false
		}
		if r.anchored || len(patternParts) > 1 {
			return matchFromRoot(target, patternParts)
		}
		for _, part := range target {
			if ok, _ := path.Match(r.pattern, part); ok {
				return true
			}
		}
		return false
	}

	if r.anchored || len(patternParts) > 1 {
		return matchFromRoot(parts, patternParts)
	}
	for _, part := range parts {
		if ok, _ := path.Match(r.pattern, part); ok {
			return true
		}
	}
	return false
}

// matchFromRoot is the recursive ** handler — ported from
// _match_from_root in mempalace/miner.py:159-178. ** matches zero or
// more path segments; everything else falls through to path.Match which
// is fnmatch-compatible for *, ?, and [class] operators.
func matchFromRoot(target, pattern []string) bool {
	var rec func(ti, pi int) bool
	rec = func(ti, pi int) bool {
		if pi == len(pattern) {
			return true
		}
		if ti == len(target) {
			for _, p := range pattern[pi:] {
				if p != "**" {
					return false
				}
			}
			return true
		}
		p := pattern[pi]
		if p == "**" {
			return rec(ti, pi+1) || rec(ti+1, pi)
		}
		if ok, _ := path.Match(p, target[ti]); !ok {
			return false
		}
		return rec(ti+1, pi+1)
	}
	return rec(0, 0)
}

// isGitignored walks the ancestor-ordered matcher chain and returns the
// last non-nil decision — mirrors is_gitignored from miner.py:188-195.
// A missing decision defaults to "not ignored".
func isGitignored(target string, matchers []*GitignoreMatcher, isDir bool) bool {
	ignored := false
	for _, m := range matchers {
		if d := m.Matches(target, isDir); d != nil {
			ignored = *d
		}
	}
	return ignored
}
