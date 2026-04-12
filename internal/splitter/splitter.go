// Package splitter splits concatenated transcript mega-files into per-session
// files. Ports split_mega_files.py.
package splitter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// SplitOptions controls a Split call.
type SplitOptions struct {
	OutputDir   string
	DryRun      bool
	MinSessions int
	MaxFileSize int64
}

// SplitResult is the outcome of splitting one mega-file.
type SplitResult struct {
	SourceFile    string
	SessionsFound int
	FilesWritten  int
	OutputPaths   []string
}

const defaultMaxFileSize = 500 * 1024 * 1024 // 500 MB

var defaultKnownPeople = []string{"Alice", "Ben", "Riley", "Max", "Sam", "Devon", "Jordan"}

var knownNamesPath string

func init() {
	home, err := os.UserHomeDir()
	if err == nil {
		knownNamesPath = filepath.Join(home, ".mempalace", "known_names.json")
	}
}

// Split scans dir for .txt files and splits any that contain multiple sessions.
func Split(dir string, opts SplitOptions) ([]SplitResult, error) {
	if opts.MinSessions <= 0 {
		opts.MinSessions = 2
	}
	maxSize := opts.MaxFileSize
	if maxSize <= 0 {
		maxSize = defaultMaxFileSize
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("splitter: read dir: %w", err)
	}

	// Sort entries by name for deterministic output.
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	type candidate struct {
		path      string
		nSessions int
	}
	var megaFiles []candidate

	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		info, err := e.Info()
		if err != nil {
			continue
		}
		if info.Size() > maxSize {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		lines := strings.SplitAfter(string(data), "\n")
		boundaries := FindSessionBoundaries(lines)
		if len(boundaries) >= opts.MinSessions {
			megaFiles = append(megaFiles, candidate{path: path, nSessions: len(boundaries)})
		}
	}

	if len(megaFiles) == 0 {
		return nil, nil
	}

	outDir := opts.OutputDir
	var results []SplitResult
	for _, mf := range megaFiles {
		if outDir == "" {
			outDir = filepath.Dir(mf.path)
		}
		r, err := splitFile(mf.path, outDir, opts.DryRun)
		if err != nil {
			return results, err
		}
		results = append(results, r)
	}
	return results, nil
}

func splitFile(path, outputDir string, dryRun bool) (SplitResult, error) {
	maxSize := int64(defaultMaxFileSize)
	info, err := os.Stat(path)
	if err != nil {
		return SplitResult{}, fmt.Errorf("splitter: stat %s: %w", path, err)
	}
	if info.Size() > maxSize {
		return SplitResult{SourceFile: path}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return SplitResult{}, fmt.Errorf("splitter: read %s: %w", path, err)
	}
	lines := strings.SplitAfter(string(data), "\n")

	boundaries := FindSessionBoundaries(lines)
	if len(boundaries) < 2 {
		return SplitResult{SourceFile: path, SessionsFound: len(boundaries)}, nil
	}

	// Sentinel at end.
	boundaries = append(boundaries, len(lines))

	knownPeople := loadKnownPeople()
	result := SplitResult{
		SourceFile:    path,
		SessionsFound: len(boundaries) - 1,
	}

	stem := filepath.Base(path)
	stem = strings.TrimSuffix(stem, filepath.Ext(stem))
	stemSafe := reNonWord.ReplaceAllString(stem, "_")
	if len(stemSafe) > 40 {
		stemSafe = stemSafe[:40]
	}

	for i := 0; i < len(boundaries)-1; i++ {
		start := boundaries[i]
		end := boundaries[i+1]
		chunk := lines[start:end]
		if len(chunk) < 10 {
			continue
		}

		tsHuman, _ := ExtractTimestamp(chunk)
		people := ExtractPeople(chunk, knownPeople)
		subject := ExtractSubject(chunk)

		tsPart := tsHuman
		if tsPart == "" {
			tsPart = fmt.Sprintf("part%02d", i+1)
		}
		peoplePart := "unknown"
		if len(people) > 0 {
			p := people
			if len(p) > 3 {
				p = p[:3]
			}
			peoplePart = strings.Join(p, "-")
		}

		name := fmt.Sprintf("%s__%s_%s_%s.txt", stemSafe, tsPart, peoplePart, subject)
		name = reSanitize.ReplaceAllString(name, "_")
		name = reMultiUnderscore.ReplaceAllString(name, "_")

		outPath := filepath.Join(outputDir, name)
		if !dryRun {
			if err := os.WriteFile(outPath, []byte(strings.Join(chunk, "")), 0o644); err != nil {
				return result, fmt.Errorf("splitter: write %s: %w", outPath, err)
			}
		}
		result.FilesWritten++
		result.OutputPaths = append(result.OutputPaths, outPath)
	}

	// Rename original to .mega_backup if we wrote files.
	if !dryRun && result.FilesWritten > 0 {
		backup := strings.TrimSuffix(path, filepath.Ext(path)) + ".mega_backup"
		_ = os.Rename(path, backup)
	}

	return result, nil
}

var (
	reSanitize        = regexp.MustCompile(`[^\w.\-]`)
	reMultiUnderscore = regexp.MustCompile(`_+`)
	reNonWord         = regexp.MustCompile(`[^\w\-]`)
)

// FindSessionBoundaries returns line indices where true new sessions begin.
func FindSessionBoundaries(lines []string) []int {
	var boundaries []int
	for i, line := range lines {
		if strings.Contains(line, "Claude Code v") && isTrueSessionStart(lines, i) {
			boundaries = append(boundaries, i)
		}
	}
	return boundaries
}

func isTrueSessionStart(lines []string, idx int) bool {
	end := idx + 6
	if end > len(lines) {
		end = len(lines)
	}
	nearby := strings.Join(lines[idx:end], "")
	return !strings.Contains(nearby, "Ctrl+E") && !strings.Contains(nearby, "previous messages")
}

var tsPattern = regexp.MustCompile(`⏺\s+(\d{1,2}:\d{2}\s+[AP]M)\s+\w+,\s+(\w+)\s+(\d{1,2}),\s+(\d{4})`)

var months = map[string]string{
	"January": "01", "February": "02", "March": "03", "April": "04",
	"May": "05", "June": "06", "July": "07", "August": "08",
	"September": "09", "October": "10", "November": "11", "December": "12",
}

// ExtractTimestamp finds the first timestamp in the first 50 lines.
// Returns (humanStr, isoStr).
func ExtractTimestamp(lines []string) (string, string) {
	limit := 50
	if len(lines) < limit {
		limit = len(lines)
	}
	for _, line := range lines[:limit] {
		m := tsPattern.FindStringSubmatch(line)
		if m != nil {
			timeStr, month, day, year := m[1], m[2], m[3], m[4]
			mon := months[month]
			if mon == "" {
				mon = "00"
			}
			dayZ := day
			if len(dayZ) == 1 {
				dayZ = "0" + dayZ
			}
			timeSafe := strings.ReplaceAll(strings.ReplaceAll(timeStr, ":", ""), " ", "")
			iso := fmt.Sprintf("%s-%s-%s", year, mon, dayZ)
			human := fmt.Sprintf("%s-%s-%s_%s", year, mon, dayZ, timeSafe)
			return human, iso
		}
	}
	return "", ""
}

// ExtractPeople detects known people mentioned in the first 100 lines.
func ExtractPeople(lines []string, knownPeople []string) []string {
	limit := 100
	if len(lines) < limit {
		limit = len(lines)
	}
	text := strings.Join(lines[:limit], "")

	found := map[string]bool{}
	for _, person := range knownPeople {
		re := regexp.MustCompile(`(?i)\b` + regexp.QuoteMeta(person) + `\b`)
		if re.MatchString(text) {
			found[person] = true
		}
	}

	// Username directory hint.
	dirMatch := regexp.MustCompile(`/Users/(\w+)/`).FindStringSubmatch(text)
	if dirMatch != nil {
		usernameMap := loadUsernameMap()
		if name, ok := usernameMap[dirMatch[1]]; ok {
			found[name] = true
		}
	}

	out := make([]string, 0, len(found))
	for p := range found {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

var skipSubjectRe = regexp.MustCompile(`^(\.\/|cd |ls |python|bash|git |cat |source |export |claude|./activate)`)

// ExtractSubject finds the first meaningful user prompt ("> " line).
func ExtractSubject(lines []string) string {
	for _, line := range lines {
		if strings.HasPrefix(line, "> ") {
			prompt := strings.TrimSpace(line[2:])
			if prompt == "" || skipSubjectRe.MatchString(prompt) || len(prompt) <= 5 {
				continue
			}
			subject := regexp.MustCompile(`[^\w\s-]`).ReplaceAllString(prompt, "")
			subject = regexp.MustCompile(`\s+`).ReplaceAllString(strings.TrimSpace(subject), "-")
			if len(subject) > 60 {
				subject = subject[:60]
			}
			return subject
		}
	}
	return "session"
}

func loadKnownPeople() []string {
	cfg := loadKnownNamesConfig()
	if cfg == nil {
		return append([]string(nil), defaultKnownPeople...)
	}
	// Could be a list.
	var list []string
	if err := json.Unmarshal(cfg, &list); err == nil && len(list) > 0 {
		return list
	}
	// Could be a dict with "names" key.
	var dict map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &dict); err == nil {
		if raw, ok := dict["names"]; ok {
			if err := json.Unmarshal(raw, &list); err == nil && len(list) > 0 {
				return list
			}
		}
	}
	return append([]string(nil), defaultKnownPeople...)
}

func loadUsernameMap() map[string]string {
	cfg := loadKnownNamesConfig()
	if cfg == nil {
		return nil
	}
	var dict map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &dict); err != nil {
		return nil
	}
	raw, ok := dict["username_map"]
	if !ok {
		return nil
	}
	var m map[string]string
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

func loadKnownNamesConfig() json.RawMessage {
	if knownNamesPath == "" {
		return nil
	}
	data, err := os.ReadFile(knownNamesPath)
	if err != nil {
		return nil
	}
	return json.RawMessage(data)
}
