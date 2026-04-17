// Package miner walks a project directory, chunks every readable file,
// and files the results into a palace. Port of mempalace/miner.py minus
// chromadb bindings — I/O goes through internal/palace and internal/embed.
package miner

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/guerra/go-palace/internal/normalize"
	"github.com/guerra/go-palace/internal/room"
	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/palace"
)

// MineOptions configures one Mine() invocation. Workers is a RESERVED
// future field — Phase B is strictly sequential so output matches the
// Python oracle line-for-line.
type MineOptions struct {
	ProjectDir       string
	PalacePath       string
	WingOverride     string
	Agent            string
	Limit            int
	DryRun           bool
	RespectGitignore bool
	IncludeIgnored   []string
	Workers          int // RESERVED: ignored in Phase B (see arch Decision 3)
	Stdout           io.Writer
}

// Mine runs the full mine pipeline. When opts.DryRun is true the palace
// pointer may be nil — Mine MUST NOT call any palace method in dry-run
// mode. The banner + per-file + summary output exactly mirrors Python's
// stdout template so behavioral-suite regex tests pass against both
// implementations.
func Mine(opts MineOptions, p *palace.Palace) error {
	if opts.Agent == "" {
		opts.Agent = "mempalace"
	}
	out := opts.Stdout
	if out == nil {
		out = os.Stdout
	}

	absDir, err := filepath.Abs(opts.ProjectDir)
	if err != nil {
		return fmt.Errorf("mine: abs %s: %w", opts.ProjectDir, err)
	}

	wing, rooms, err := room.LoadConfig(absDir)
	if err != nil {
		return fmt.Errorf("mine: %w", err)
	}
	if opts.WingOverride != "" {
		wing = opts.WingOverride
	}

	files, err := ScanProject(absDir, ScanOptions{
		RespectGitignore: opts.RespectGitignore,
		IncludeIgnored:   opts.IncludeIgnored,
	})
	if err != nil {
		return fmt.Errorf("mine: scan: %w", err)
	}
	if opts.Limit > 0 && opts.Limit < len(files) {
		files = files[:opts.Limit]
	}

	roomNames := make([]string, len(rooms))
	for i, r := range rooms {
		roomNames[i] = r.Name
	}

	bar55 := strings.Repeat("=", 55)
	rule55 := strings.Repeat("─", 55)

	fmt.Fprintf(out, "\n%s\n", bar55)
	fmt.Fprintln(out, "  MemPalace Mine")
	fmt.Fprintln(out, bar55)
	fmt.Fprintf(out, "  Wing:    %s\n", wing)
	fmt.Fprintf(out, "  Rooms:   %s\n", strings.Join(roomNames, ", "))
	fmt.Fprintf(out, "  Files:   %d\n", len(files))
	fmt.Fprintf(out, "  Palace:  %s\n", opts.PalacePath)
	if opts.DryRun {
		fmt.Fprintln(out, "  DRY RUN — nothing will be filed")
	}
	if !opts.RespectGitignore {
		fmt.Fprintln(out, "  .gitignore: DISABLED")
	}
	if len(opts.IncludeIgnored) > 0 {
		includeSet := normalizeIncludePaths(opts.IncludeIgnored)
		sortedIncludes := make([]string, 0, len(includeSet))
		for k := range includeSet {
			sortedIncludes = append(sortedIncludes, k)
		}
		sort.Strings(sortedIncludes)
		fmt.Fprintf(out, "  Include: %s\n", strings.Join(sortedIncludes, ", "))
	}
	fmt.Fprintf(out, "%s\n\n", rule55)

	if !opts.DryRun && p == nil {
		return fmt.Errorf("mine: palace required for non-dry-run")
	}

	var (
		totalDrawers int
		filesSkipped int
		roomCounts   = map[string]int{}
	)

	for i, src := range files {
		info, err := os.Stat(src)
		if err != nil {
			slog.Debug("mine: skip file (stat error)", "file", src, "err", err)
			filesSkipped++
			continue
		}

		if !opts.DryRun && alreadyMined(p, src, info) {
			slog.Debug("mine: skip file (already mined)", "file", src)
			filesSkipped++
			continue
		}

		// SEC-014: open with O_NOFOLLOW to prevent TOCTOU symlink swap between
		// scan and read. If the path became a symlink after scanning, this fails.
		if err := verifyNotSymlink(src); err != nil {
			slog.Debug("mine: skip file (symlink check)", "file", src, "err", err)
			filesSkipped++
			continue
		}

		content, err := normalize.Normalize(src)
		if err != nil {
			slog.Debug("mine: skip file (normalize error)", "file", src, "err", err)
			filesSkipped++
			continue
		}
		content = strings.TrimSpace(content)
		if len(content) < MinChunkSize {
			slog.Debug("mine: skip file (below min chunk size)", "file", src, "len", len(content))
			filesSkipped++
			continue
		}

		rel, _ := filepath.Rel(absDir, src)
		roomName := DetectRoom(rel, content, rooms)
		chunks := ChunkText(content)

		if opts.DryRun {
			fmt.Fprintf(out, "    [DRY RUN] %s → room:%s (%d drawers)\n",
				filepath.Base(src), roomName, len(chunks))
			totalDrawers += len(chunks)
			roomCounts[roomName]++
			continue
		}

		if len(chunks) == 0 {
			filesSkipped++
			continue
		}

		// Purge stale drawers for this source before re-inserting fresh
		// chunks — mirrors miner.py:444-447's delete(where=...) dance.
		existing, err := p.Get(palace.GetOptions{
			Where: map[string]string{"source_file": src},
		})
		if err == nil {
			for _, d := range existing {
				if delErr := p.Delete(d.ID); delErr != nil {
					return fmt.Errorf("mine: delete stale drawer %s: %w", d.ID, delErr)
				}
			}
		}

		drawers := make([]palace.Drawer, 0, len(chunks))
		now := time.Now()
		mtime := float64(info.ModTime().UnixNano()) / 1e9
		for _, c := range chunks {
			drawers = append(drawers, palace.Drawer{
				ID:          palace.ComputeDrawerID(wing, roomName, src, c.Index),
				Document:    c.Content,
				Wing:        wing,
				Hall:        halls.Detect(c.Content, roomName, opts.Agent, nil),
				Room:        roomName,
				SourceFile:  src,
				ChunkIndex:  c.Index,
				AddedBy:     opts.Agent,
				FiledAt:     now,
				SourceMTime: mtime,
			})
		}
		if err := p.UpsertBatch(drawers); err != nil {
			return fmt.Errorf("mine: upsert %s: %w", src, err)
		}

		basename := filepath.Base(src)
		if len(basename) > 50 {
			basename = basename[:50]
		}
		fmt.Fprintf(out, "  ✓ [%4d/%d] %-50s +%d\n", i+1, len(files), basename, len(chunks))
		totalDrawers += len(chunks)
		roomCounts[roomName]++
	}

	fmt.Fprintf(out, "\n%s\n", bar55)
	fmt.Fprintln(out, "  Done.")
	fmt.Fprintf(out, "  Files processed: %d\n", len(files)-filesSkipped)
	fmt.Fprintf(out, "  Files skipped (already filed): %d\n", filesSkipped)
	fmt.Fprintf(out, "  Drawers filed: %d\n", totalDrawers)
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
	fmt.Fprintln(out, "\n  Next: mempalace search \"what you're looking for\"")
	fmt.Fprintf(out, "%s\n\n", bar55)

	return nil
}

// alreadyMined checks the palace for a drawer pointing at sourceFile and
// compares mtimes at float64 granularity — Python-identical semantics so
// re-mining a project skips unchanged files, port of file_already_mined
// in mempalace/palace.py:51-71.
func alreadyMined(p *palace.Palace, sourceFile string, info os.FileInfo) bool {
	drawers, err := p.Get(palace.GetOptions{
		Where: map[string]string{"source_file": sourceFile},
		Limit: 1,
	})
	if err != nil || len(drawers) == 0 {
		return false
	}
	current := float64(info.ModTime().UnixNano()) / 1e9
	return drawers[0].SourceMTime == current
}

// Status prints a wing/room breakdown of drawers currently in the palace.
// Phase B wires this into internal/miner for reuse; the CLI `status`
// verb stays on the Phase A stub until Phase C.
func Status(palacePath string, stdout io.Writer, p *palace.Palace) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	drawers, err := p.Get(palace.GetOptions{Limit: 10000})
	if err != nil {
		return fmt.Errorf("status: get drawers: %w", err)
	}
	wingRooms := map[string]map[string]int{}
	for _, d := range drawers {
		if wingRooms[d.Wing] == nil {
			wingRooms[d.Wing] = map[string]int{}
		}
		wingRooms[d.Wing][d.Room]++
	}

	bar := strings.Repeat("=", 55)
	fmt.Fprintf(stdout, "\n%s\n", bar)
	fmt.Fprintf(stdout, "  MemPalace Status — %d drawers\n", len(drawers))
	fmt.Fprintf(stdout, "%s\n\n", bar)

	wings := make([]string, 0, len(wingRooms))
	for w := range wingRooms {
		wings = append(wings, w)
	}
	sort.Strings(wings)
	for _, w := range wings {
		fmt.Fprintf(stdout, "  WING: %s\n", w)
		type rc struct {
			name  string
			count int
		}
		var rs []rc
		for n, c := range wingRooms[w] {
			rs = append(rs, rc{n, c})
		}
		sort.Slice(rs, func(i, j int) bool {
			if rs[i].count != rs[j].count {
				return rs[i].count > rs[j].count
			}
			return rs[i].name < rs[j].name
		})
		for _, r := range rs {
			fmt.Fprintf(stdout, "    ROOM: %-20s %5d drawers\n", r.name, r.count)
		}
		fmt.Fprintln(stdout)
	}
	fmt.Fprintf(stdout, "%s\n\n", bar)
	return nil
}
