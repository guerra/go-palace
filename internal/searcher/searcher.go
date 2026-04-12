package searcher

import (
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"

	"go-palace/internal/palace"
)

type SearchOptions struct {
	Query      string
	Wing       string
	Room       string
	NResults   int
	PalacePath string
}

type SearchResult struct {
	Text       string
	Wing       string
	Room       string
	SourceFile string
	Similarity float64
}

// Search runs a semantic query and writes formatted results to w,
// matching the Python searcher.py stdout format.
func Search(p *palace.Palace, opts SearchOptions, w io.Writer) error {
	n := opts.NResults
	if n <= 0 {
		n = 5
	}

	results, err := p.Query(opts.Query, palace.QueryOptions{
		Wing:     opts.Wing,
		Room:     opts.Room,
		NResults: n,
	})
	if err != nil {
		return fmt.Errorf("searcher: query: %w", err)
	}

	if len(results) == 0 {
		fmt.Fprintf(w, "\n  No results found for: %q\n", opts.Query)
		return nil
	}

	bar := strings.Repeat("=", 60)
	fmt.Fprintf(w, "\n%s\n", bar)
	fmt.Fprintf(w, "  Results for: %q\n", opts.Query)
	if opts.Wing != "" {
		fmt.Fprintf(w, "  Wing: %s\n", opts.Wing)
	}
	if opts.Room != "" {
		fmt.Fprintf(w, "  Room: %s\n", opts.Room)
	}
	fmt.Fprintf(w, "%s\n\n", bar)

	for i, r := range results {
		sim := math.Round(r.Similarity*1000) / 1000
		source := filepath.Base(r.Drawer.SourceFile)
		wing := r.Drawer.Wing
		room := r.Drawer.Room
		if wing == "" {
			wing = "?"
		}
		if room == "" {
			room = "?"
		}
		if source == "" || source == "." {
			source = "?"
		}

		fmt.Fprintf(w, "  [%d] %s / %s\n", i+1, wing, room)
		fmt.Fprintf(w, "      Source: %s\n", source)
		fmt.Fprintf(w, "      Match:  %g\n", sim)
		fmt.Fprintln(w)
		for _, line := range strings.Split(strings.TrimSpace(r.Drawer.Document), "\n") {
			fmt.Fprintf(w, "      %s\n", line)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", strings.Repeat("\u2500", 56))
	}

	fmt.Fprintln(w)
	return nil
}

// SearchMemories runs a semantic query and returns structured results,
// matching the Python search_memories return format.
func SearchMemories(p *palace.Palace, opts SearchOptions) ([]SearchResult, error) {
	n := opts.NResults
	if n <= 0 {
		n = 5
	}

	results, err := p.Query(opts.Query, palace.QueryOptions{
		Wing:     opts.Wing,
		Room:     opts.Room,
		NResults: n,
	})
	if err != nil {
		return nil, fmt.Errorf("searcher: query: %w", err)
	}

	out := make([]SearchResult, 0, len(results))
	for _, r := range results {
		source := filepath.Base(r.Drawer.SourceFile)
		if source == "" || source == "." {
			source = "?"
		}
		wing := r.Drawer.Wing
		if wing == "" {
			wing = "unknown"
		}
		room := r.Drawer.Room
		if room == "" {
			room = "unknown"
		}
		out = append(out, SearchResult{
			Text:       r.Drawer.Document,
			Wing:       wing,
			Room:       room,
			SourceFile: source,
			Similarity: math.Round(r.Similarity*1000) / 1000,
		})
	}
	return out, nil
}
