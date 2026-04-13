package layers

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/guerra/go-palace/pkg/palace"
)

const (
	MaxDrawers = 15
	MaxChars   = 3200
	batchSize  = 500
)

type Stack struct {
	palace       *palace.Palace
	identityPath string
	l0cache      *string
}

func NewStack(p *palace.Palace, identityPath string) *Stack {
	if identityPath == "" {
		home, _ := os.UserHomeDir()
		identityPath = filepath.Join(home, ".mempalace", "identity.txt")
	}
	return &Stack{palace: p, identityPath: identityPath}
}

// L0 returns the identity text from identityPath. Cached after first read.
func (s *Stack) L0() string {
	if s.l0cache != nil {
		return *s.l0cache
	}
	data, err := os.ReadFile(s.identityPath)
	var text string
	if err != nil {
		text = "## L0 — IDENTITY\nNo identity configured. Create ~/.mempalace/identity.txt"
	} else {
		text = strings.TrimSpace(string(data))
	}
	s.l0cache = &text
	return text
}

type scoredDrawer struct {
	importance float64
	drawer     palace.Drawer
}

// L1 returns the essential story summary from the top drawers.
func (s *Stack) L1() string {
	return s.l1(nil)
}

// L1Wing returns L1 filtered by wing.
func (s *Stack) L1Wing(wing string) string {
	where := map[string]string{"wing": wing}
	return s.l1(where)
}

func (s *Stack) l1(where map[string]string) string {
	var all []palace.Drawer
	offset := 0
	for {
		batch, err := s.palace.Get(palace.GetOptions{
			Where:  where,
			Limit:  batchSize,
			Offset: offset,
		})
		if err != nil || len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		offset += len(batch)
		if len(batch) < batchSize {
			break
		}
	}

	if len(all) == 0 {
		return "## L1 — No memories yet."
	}

	scored := make([]scoredDrawer, len(all))
	for i, d := range all {
		importance := 3.0
		for _, key := range []string{"importance", "emotional_weight", "weight"} {
			val, ok := d.Metadata[key]
			if !ok {
				continue
			}
			switch v := val.(type) {
			case float64:
				importance = v
			case string:
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					importance = f
				}
			}
			break
		}
		scored[i] = scoredDrawer{importance: importance, drawer: d}
	}

	sort.SliceStable(scored, func(i, j int) bool {
		return scored[i].importance > scored[j].importance
	})
	if len(scored) > MaxDrawers {
		scored = scored[:MaxDrawers]
	}

	byRoom := make(map[string][]scoredDrawer)
	for _, sd := range scored {
		room := sd.drawer.Room
		if room == "" {
			room = "general"
		}
		byRoom[room] = append(byRoom[room], sd)
	}

	roomKeys := make([]string, 0, len(byRoom))
	for k := range byRoom {
		roomKeys = append(roomKeys, k)
	}
	sort.Strings(roomKeys)

	lines := []string{"## L1 — ESSENTIAL STORY"}
	totalLen := 0

	for _, room := range roomKeys {
		roomLine := fmt.Sprintf("\n[%s]", room)
		lines = append(lines, roomLine)
		totalLen += len(roomLine)

		for _, sd := range byRoom[room] {
			source := ""
			if sd.drawer.SourceFile != "" {
				source = filepath.Base(sd.drawer.SourceFile)
			}

			snippet := strings.ReplaceAll(strings.TrimSpace(sd.drawer.Document), "\n", " ")
			if len(snippet) > 200 {
				snippet = snippet[:197] + "..."
			}

			entryLine := fmt.Sprintf("  - %s", snippet)
			if source != "" {
				entryLine += fmt.Sprintf("  (%s)", source)
			}

			if totalLen+len(entryLine) > MaxChars {
				lines = append(lines, "  ... (more in L3 search)")
				return strings.Join(lines, "\n")
			}

			lines = append(lines, entryLine)
			totalLen += len(entryLine)
		}
	}

	return strings.Join(lines, "\n")
}

// L2 returns on-demand drawers filtered by wing/room.
func (s *Stack) L2(wing, room string) string {
	where := make(map[string]string)
	if wing != "" {
		where["wing"] = wing
	}
	if room != "" {
		where["room"] = room
	}

	drawers, err := s.palace.Get(palace.GetOptions{
		Where: where,
		Limit: 10,
	})
	if err != nil || len(drawers) == 0 {
		label := ""
		if wing != "" {
			label = "wing=" + wing
		}
		if room != "" {
			if label != "" {
				label += " "
			}
			label += "room=" + room
		}
		return fmt.Sprintf("No drawers found for %s.", label)
	}

	lines := []string{fmt.Sprintf("## L2 — ON-DEMAND (%d drawers)", len(drawers))}
	for _, d := range drawers {
		roomName := d.Room
		if roomName == "" {
			roomName = "?"
		}
		source := ""
		if d.SourceFile != "" {
			source = filepath.Base(d.SourceFile)
		}
		snippet := strings.ReplaceAll(strings.TrimSpace(d.Document), "\n", " ")
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}
		entry := fmt.Sprintf("  [%s] %s", roomName, snippet)
		if source != "" {
			entry += fmt.Sprintf("  (%s)", source)
		}
		lines = append(lines, entry)
	}

	return strings.Join(lines, "\n")
}

// L3 returns semantic search results.
func (s *Stack) L3(query string, wing string, room string) string {
	results, err := s.palace.Query(query, palace.QueryOptions{
		Wing:     wing,
		Room:     room,
		NResults: 5,
	})
	if err != nil || len(results) == 0 {
		return "No results found."
	}

	lines := []string{fmt.Sprintf("## L3 — SEARCH RESULTS for %q", query)}
	for i, r := range results {
		sim := math.Round(r.Similarity*1000) / 1000
		wing := r.Drawer.Wing
		if wing == "" {
			wing = "?"
		}
		room := r.Drawer.Room
		if room == "" {
			room = "?"
		}
		source := ""
		if r.Drawer.SourceFile != "" {
			source = filepath.Base(r.Drawer.SourceFile)
		}

		snippet := strings.ReplaceAll(strings.TrimSpace(r.Drawer.Document), "\n", " ")
		if len(snippet) > 300 {
			snippet = snippet[:297] + "..."
		}

		lines = append(lines, fmt.Sprintf("  [%d] %s/%s (sim=%g)", i+1, wing, room, sim))
		lines = append(lines, fmt.Sprintf("      %s", snippet))
		if source != "" {
			lines = append(lines, fmt.Sprintf("      src: %s", source))
		}
	}

	return strings.Join(lines, "\n")
}

// WakeUp returns L0 + L1 combined.
func (s *Stack) WakeUp() string {
	return s.L0() + "\n\n" + s.L1()
}

// WakeUpWing returns L0 + L1 filtered by wing.
func (s *Stack) WakeUpWing(wing string) string {
	return s.L0() + "\n\n" + s.L1Wing(wing)
}

// TokenEstimate approximates token count as len/4.
func TokenEstimate(text string) int {
	return len(text) / 4
}
