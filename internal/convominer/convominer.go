// Package convominer scans conversation directories, normalizes chat exports,
// chunks by exchange pair (Q+A = one unit), and files results into a palace.
// Port of convo_miner.py. Does NOT import the miner package — different
// chunking strategy for conversations vs project files.
package convominer

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"go-palace/internal/extractor"
	"go-palace/internal/normalize"
	"go-palace/pkg/palace"
)

// File type and size constants — ported from convo_miner.py:23-31.
var convoExtensions = map[string]struct{}{
	".txt":   {},
	".md":    {},
	".json":  {},
	".jsonl": {},
}

const (
	MinChunkSize = 30
	MaxFileSize  = 10 * 1024 * 1024 // 10 MB
	MaxAILines   = 8
)

// skipDirs is the convominer-side blocklist. Defined locally — does NOT import
// from miner package. Same set as Python's palace.py SKIP_DIRS.
var skipDirs = map[string]struct{}{
	".git": {}, "node_modules": {}, "__pycache__": {}, ".venv": {},
	"venv": {}, "env": {}, "dist": {}, "build": {}, ".next": {},
	"coverage": {}, ".mempalace": {}, ".ruff_cache": {},
	".mypy_cache": {}, ".pytest_cache": {}, ".cache": {}, ".tox": {},
	".nox": {}, ".idea": {}, ".vscode": {}, ".ipynb_checkpoints": {},
	".eggs": {}, "htmlcov": {}, "target": {},
}

// Chunk is one drawer-sized slice of text from a conversation.
type Chunk struct {
	Content    string
	ChunkIndex int
	MemoryType string // only set when extract_mode="general"
}

// ConvoMineOptions configures one MineConvos invocation.
type ConvoMineOptions struct {
	ConvoDir     string
	PalacePath   string
	WingOverride string
	Agent        string
	Limit        int
	DryRun       bool
	ExtractMode  string // "exchange" or "general"
	Stdout       io.Writer
}

// TopicKeywords maps room names to keyword lists for conversation room detection.
// Port of convo_miner.py:114-178.
var TopicKeywords = map[string][]string{
	"technical": {
		"code", "python", "function", "bug", "error", "api", "database",
		"server", "deploy", "git", "test", "debug", "refactor",
	},
	"architecture": {
		"architecture", "design", "pattern", "structure", "schema",
		"interface", "module", "component", "service", "layer",
	},
	"planning": {
		"plan", "roadmap", "milestone", "deadline", "priority",
		"sprint", "backlog", "scope", "requirement", "spec",
	},
	"decisions": {
		"decided", "chose", "picked", "switched", "migrated",
		"replaced", "trade-off", "alternative", "option", "approach",
	},
	"problems": {
		"problem", "issue", "broken", "failed", "crash",
		"stuck", "workaround", "fix", "solved", "resolved",
	},
}

// ScanConvos finds all potential conversation files in a directory.
// Port of scan_convos in convo_miner.py:204-224.
func ScanConvos(dir string) ([]string, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("scan convos: abs %s: %w", dir, err)
	}
	var files []string
	err = filepath.WalkDir(absDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			if path == absDir {
				return nil
			}
			if _, skip := skipDirs[d.Name()]; skip {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip .meta.json files.
		if strings.HasSuffix(d.Name(), ".meta.json") {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := convoExtensions[ext]; !ok {
			return nil
		}
		// Skip symlinks.
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
		files = append(files, path)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("scan convos: walk: %w", err)
	}
	sort.Strings(files)
	return files, nil
}

// ChunkExchanges chunks content by exchange pair: one > turn + AI response = one unit.
// Falls back to paragraph chunking if fewer than 3 ">" markers.
// Port of chunk_exchanges in convo_miner.py:40-107.
func ChunkExchanges(content string) []Chunk {
	lines := strings.Split(content, "\n")
	quoteLines := 0
	for _, line := range lines {
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			quoteLines++
		}
	}
	if quoteLines >= 3 {
		return chunkByExchange(lines)
	}
	return chunkByParagraph(content)
}

func chunkByExchange(lines []string) []Chunk {
	var chunks []Chunk
	i := 0
	for i < len(lines) {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), ">") {
			userTurn := strings.TrimSpace(line)
			i++

			var aiLines []string
			for i < len(lines) {
				nextLine := lines[i]
				trimmed := strings.TrimSpace(nextLine)
				if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "---") {
					break
				}
				if trimmed != "" {
					aiLines = append(aiLines, trimmed)
				}
				i++
			}

			aiResponse := strings.Join(aiLines[:min(len(aiLines), MaxAILines)], " ")
			var chunkContent string
			if aiResponse != "" {
				chunkContent = userTurn + "\n" + aiResponse
			} else {
				chunkContent = userTurn
			}

			if len(strings.TrimSpace(chunkContent)) > MinChunkSize {
				chunks = append(chunks, Chunk{
					Content:    chunkContent,
					ChunkIndex: len(chunks),
				})
			}
		} else {
			i++
		}
	}
	return chunks
}

func chunkByParagraph(content string) []Chunk {
	var chunks []Chunk
	paragraphs := strings.Split(content, "\n\n")

	// If no paragraph breaks and long content, chunk by line groups.
	var nonEmpty []string
	for _, p := range paragraphs {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(p))
		}
	}

	if len(nonEmpty) <= 1 && strings.Count(content, "\n") > 20 {
		lines := strings.Split(content, "\n")
		for i := 0; i < len(lines); i += 25 {
			end := i + 25
			if end > len(lines) {
				end = len(lines)
			}
			group := strings.TrimSpace(strings.Join(lines[i:end], "\n"))
			if len(group) > MinChunkSize {
				chunks = append(chunks, Chunk{Content: group, ChunkIndex: len(chunks)})
			}
		}
		return chunks
	}

	for _, para := range nonEmpty {
		if len(para) > MinChunkSize {
			chunks = append(chunks, Chunk{Content: para, ChunkIndex: len(chunks)})
		}
	}
	return chunks
}

// DetectConvoRoom scores conversation content against topic keywords.
// Port of detect_convo_room in convo_miner.py:181-191.
func DetectConvoRoom(content string) string {
	upper := 3000
	if len(content) < upper {
		upper = len(content)
	}
	contentLower := strings.ToLower(content[:upper])

	scores := make(map[string]int)
	for room, keywords := range TopicKeywords {
		score := 0
		for _, kw := range keywords {
			if strings.Contains(contentLower, kw) {
				score++
			}
		}
		if score > 0 {
			scores[room] = score
		}
	}
	if len(scores) == 0 {
		return "general"
	}

	bestRoom := ""
	bestScore := 0
	// Sort room names for determinism when scores tie.
	rooms := make([]string, 0, len(scores))
	for r := range scores {
		rooms = append(rooms, r)
	}
	sort.Strings(rooms)
	for _, r := range rooms {
		if scores[r] > bestScore {
			bestScore = scores[r]
			bestRoom = r
		}
	}
	return bestRoom
}

// wingFromDir derives a wing name from directory path.
// Mirrors projectNameFromDir in main.go — duplicated to avoid importing main.
func wingFromDir(absDir string) string {
	base := filepath.Base(absDir)
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, " ", "_")
	base = strings.ReplaceAll(base, "-", "_")
	return base
}

// alreadyMined checks the palace for existing drawers with this source_file.
// Unlike project miner, convominer does existence-only check (no mtime).
// Port of convo_miner.py:277-278.
func alreadyMined(p *palace.Palace, sourceFile string) bool {
	drawers, err := p.Get(palace.GetOptions{
		Where: map[string]string{"source_file": sourceFile},
		Limit: 1,
	})
	return err == nil && len(drawers) > 0
}

// MineConvos runs the full conversation mining pipeline.
// Port of mine_convos in convo_miner.py:232-371.
func MineConvos(opts ConvoMineOptions, p *palace.Palace) error {
	if opts.Agent == "" {
		opts.Agent = "mempalace"
	}
	if opts.ExtractMode == "" {
		opts.ExtractMode = "exchange"
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}

	absDir, err := filepath.Abs(opts.ConvoDir)
	if err != nil {
		return fmt.Errorf("mine convos: abs %s: %w", opts.ConvoDir, err)
	}

	wing := opts.WingOverride
	if wing == "" {
		wing = wingFromDir(absDir)
	}

	files, err := ScanConvos(opts.ConvoDir)
	if err != nil {
		return fmt.Errorf("mine convos: scan: %w", err)
	}
	if opts.Limit > 0 && opts.Limit < len(files) {
		files = files[:opts.Limit]
	}

	bar55 := strings.Repeat("=", 55)
	rule55 := strings.Repeat("─", 55)

	fmt.Fprintf(out, "\n%s\n", bar55)
	fmt.Fprintln(out, "  MemPalace Mine — Conversations")
	fmt.Fprintln(out, bar55)
	fmt.Fprintf(out, "  Wing:    %s\n", wing)
	fmt.Fprintf(out, "  Source:  %s\n", absDir)
	fmt.Fprintf(out, "  Files:   %d\n", len(files))
	fmt.Fprintf(out, "  Palace:  %s\n", opts.PalacePath)
	if opts.DryRun {
		fmt.Fprintln(out, "  DRY RUN — nothing will be filed")
	}
	fmt.Fprintf(out, "%s\n\n", rule55)

	if !opts.DryRun && p == nil {
		return fmt.Errorf("mine convos: palace required for non-dry-run")
	}

	var (
		totalDrawers int
		filesSkipped int
		roomCounts   = map[string]int{}
	)

	for i, src := range files {
		sourceFile := src

		if !opts.DryRun && alreadyMined(p, sourceFile) {
			slog.Debug("convominer: skip file (already mined)", "file", src)
			filesSkipped++
			continue
		}

		content, err := normalize.Normalize(src)
		if err != nil {
			slog.Debug("convominer: skip file (normalize error)", "file", src, "err", err)
			continue
		}
		if len(strings.TrimSpace(content)) < MinChunkSize {
			continue
		}

		var chunks []Chunk

		if opts.ExtractMode == "general" {
			memories := extractor.ExtractMemories(content, 0.3)
			for _, m := range memories {
				chunks = append(chunks, Chunk{
					Content:    m.Content,
					ChunkIndex: m.ChunkIndex,
					MemoryType: m.MemoryType,
				})
			}
		} else {
			chunks = ChunkExchanges(content)
		}

		if len(chunks) == 0 {
			continue
		}

		// Detect room (general mode uses memory_type per-chunk).
		roomName := ""
		if opts.ExtractMode != "general" {
			roomName = DetectConvoRoom(content)
		}

		if opts.DryRun {
			if opts.ExtractMode == "general" {
				typeCounts := map[string]int{}
				for _, c := range chunks {
					mt := c.MemoryType
					if mt == "" {
						mt = "general"
					}
					typeCounts[mt]++
				}
				// Build types string sorted by count desc.
				type tc struct {
					name  string
					count int
				}
				var tcs []tc
				for t, n := range typeCounts {
					tcs = append(tcs, tc{t, n})
				}
				sort.Slice(tcs, func(a, b int) bool {
					if tcs[a].count != tcs[b].count {
						return tcs[a].count > tcs[b].count
					}
					return tcs[a].name < tcs[b].name
				})
				var parts []string
				for _, t := range tcs {
					parts = append(parts, fmt.Sprintf("%s:%d", t.name, t.count))
				}
				fmt.Fprintf(out, "    [DRY RUN] %s → %d memories (%s)\n",
					filepath.Base(src), len(chunks), strings.Join(parts, ", "))
			} else {
				fmt.Fprintf(out, "    [DRY RUN] %s → room:%s (%d drawers)\n",
					filepath.Base(src), roomName, len(chunks))
			}
			totalDrawers += len(chunks)
			if opts.ExtractMode == "general" {
				for _, c := range chunks {
					mt := c.MemoryType
					if mt == "" {
						mt = "general"
					}
					roomCounts[mt]++
				}
			} else {
				roomCounts[roomName]++
			}
			continue
		}

		// File each chunk to palace.
		if opts.ExtractMode != "general" {
			roomCounts[roomName]++
		}

		drawers := make([]palace.Drawer, 0, len(chunks))
		now := time.Now()
		for _, c := range chunks {
			chunkRoom := roomName
			if opts.ExtractMode == "general" {
				chunkRoom = c.MemoryType
				if chunkRoom == "" {
					chunkRoom = "general"
				}
				roomCounts[chunkRoom]++
			}
			// SourceMTime intentionally zero: convominer uses existence-only
			// checks (no mtime comparison), matching convo_miner.py:277-278.
			drawers = append(drawers, palace.Drawer{
				ID:         palace.ComputeDrawerID(wing, chunkRoom, sourceFile, c.ChunkIndex),
				Document:   c.Content,
				Wing:       wing,
				Room:       chunkRoom,
				SourceFile: sourceFile,
				ChunkIndex: c.ChunkIndex,
				AddedBy:    opts.Agent,
				FiledAt:    now,
				Metadata: map[string]any{
					"ingest_mode":  "convos",
					"extract_mode": opts.ExtractMode,
				},
			})
		}
		if err := p.UpsertBatch(drawers); err != nil {
			return fmt.Errorf("mine convos: upsert %s: %w", src, err)
		}

		basename := filepath.Base(src)
		if len(basename) > 50 {
			basename = basename[:50]
		}
		fmt.Fprintf(out, "  ✓ [%4d/%d] %-50s +%d\n", i+1, len(files), basename, len(drawers))
		totalDrawers += len(drawers)
	}

	fmt.Fprintf(out, "\n%s\n", bar55)
	fmt.Fprintln(out, "  Done.")
	fmt.Fprintf(out, "  Files processed: %d\n", len(files)-filesSkipped)
	fmt.Fprintf(out, "  Files skipped (already filed): %d\n", filesSkipped)
	fmt.Fprintf(out, "  Drawers filed: %d\n", totalDrawers)
	if len(roomCounts) > 0 {
		fmt.Fprintln(out, "\n  By room:")
		type rc struct {
			name  string
			count int
		}
		var ordered []rc
		for name, count := range roomCounts {
			ordered = append(ordered, rc{name, count})
		}
		sort.Slice(ordered, func(i, j int) bool {
			if ordered[i].count != ordered[j].count {
				return ordered[i].count > ordered[j].count
			}
			return ordered[i].name < ordered[j].name
		})
		for _, r := range ordered {
			fmt.Fprintf(out, "    %-20s %d files\n", r.name, r.count)
		}
	}
	fmt.Fprintf(out, "\n  Next: mempalace search \"what you're looking for\"\n")
	fmt.Fprintf(out, "%s\n\n", bar55)

	return nil
}
