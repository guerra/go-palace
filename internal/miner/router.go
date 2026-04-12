package miner

import (
	"path/filepath"
	"strings"

	"go-palace/internal/room"
)

// DetectRoom routes one file to a room using the four-priority chain
// ported from detect_room in mempalace/miner.py:278-317:
//
//  1. folder-path match against room name or keywords (substring both ways)
//  2. filename stem match against room name
//  3. keyword occurrence count in the first 2000 bytes of content
//  4. "general" fallback
func DetectRoom(relPath, content string, rooms []room.Room) string {
	relLower := strings.ToLower(filepath.ToSlash(relPath))
	pathParts := strings.Split(relLower, "/")
	// Python uses filepath.stem — the filename without its extension.
	filename := ""
	if len(pathParts) > 0 {
		last := pathParts[len(pathParts)-1]
		filename = strings.TrimSuffix(last, filepath.Ext(last))
	}

	upper := 2000
	if len(content) < upper {
		upper = len(content)
	}
	contentLower := strings.ToLower(content[:upper])

	// Priority 1: folder path parts (exclude the filename itself).
	dirParts := pathParts
	if len(dirParts) > 0 {
		dirParts = dirParts[:len(dirParts)-1]
	}
	for _, part := range dirParts {
		for _, r := range rooms {
			for _, cand := range roomCandidates(r) {
				if part == cand || strings.Contains(part, cand) || strings.Contains(cand, part) {
					return r.Name
				}
			}
		}
	}

	// Priority 2: filename stem matches room name.
	for _, r := range rooms {
		name := strings.ToLower(r.Name)
		if strings.Contains(filename, name) || (filename != "" && strings.Contains(name, filename)) {
			return r.Name
		}
	}

	// Priority 3: keyword scoring (keywords + room name, lowercase).
	bestName, bestScore := "", 0
	for _, r := range rooms {
		score := 0
		for _, kw := range append(r.Keywords, r.Name) {
			kwLower := strings.ToLower(kw)
			if kwLower == "" {
				continue
			}
			score += strings.Count(contentLower, kwLower)
		}
		if score > bestScore {
			bestScore = score
			bestName = r.Name
		}
	}
	if bestScore > 0 {
		return bestName
	}

	return "general"
}

// roomCandidates returns the lowercase set used for path-part matching:
// the room name followed by every keyword. Empty strings are filtered so
// they can't match everything via strings.Contains.
func roomCandidates(r room.Room) []string {
	out := make([]string, 0, 1+len(r.Keywords))
	if name := strings.ToLower(r.Name); name != "" {
		out = append(out, name)
	}
	for _, k := range r.Keywords {
		if kw := strings.ToLower(k); kw != "" {
			out = append(out, kw)
		}
	}
	return out
}
