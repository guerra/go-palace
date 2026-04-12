// Package dedup detects and removes near-duplicate drawers.
//
// When the same files are mined multiple times, near-identical drawers
// accumulate. This module finds drawers from the same source_file that
// are too similar (cosine distance < threshold), keeps the longest/richest
// version, and deletes the rest.
package dedup

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"go-palace/internal/palace"
)

const (
	DefaultThreshold  = 0.15
	MinDrawersToCheck = 5
	shortDocThreshold = 20
)

// DedupOptions controls how dedup scopes its work.
type DedupOptions struct {
	SourcePattern string
	MinCount      int
	Wing          string
}

// DuplicateGroup records what happened for one source_file.
type DuplicateGroup struct {
	SourceFile string
	KeptIDs    []string
	DeletedIDs []string
}

// Stats holds summary statistics for a dedup run.
type Stats struct {
	SourcesChecked int
	TotalDrawers   int
	Kept           int
	Deleted        int
}

// getSourceGroups paginates the palace and groups drawer IDs by source_file,
// returning only groups with at least minCount entries.
func getSourceGroups(p *palace.Palace, minCount int, sourcePattern string, wing string) (map[string][]string, error) {
	if minCount <= 0 {
		minCount = MinDrawersToCheck
	}
	groups := map[string][]string{}
	offset := 0
	batchSize := 1000

	for {
		where := map[string]string{}
		if wing != "" {
			where["wing"] = wing
		}
		drawers, err := p.Get(palace.GetOptions{Where: where, Limit: batchSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("dedup: get source groups: %w", err)
		}
		if len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			src := d.SourceFile
			if src == "" {
				src = "unknown"
			}
			if sourcePattern != "" && !strings.Contains(strings.ToLower(src), strings.ToLower(sourcePattern)) {
				continue
			}
			groups[src] = append(groups[src], d.ID)
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}

	// Filter to groups meeting minimum count.
	result := map[string][]string{}
	for src, ids := range groups {
		if len(ids) >= minCount {
			result[src] = ids
		}
	}
	return result, nil
}

// dedupSourceGroup performs greedy dedup within one source_file group.
// Sort by document length descending, keep if not too similar to any
// already-kept drawer.
func dedupSourceGroup(p *palace.Palace, drawerIDs []string, threshold float64, dryRun bool) (kept []string, deleted []string, err error) {
	drawers, err := p.GetByIDs(drawerIDs)
	if err != nil {
		return nil, nil, fmt.Errorf("dedup: get drawers: %w", err)
	}

	// Build a lookup for quick access.
	type item struct {
		id  string
		doc string
	}
	items := make([]item, 0, len(drawers))
	for _, d := range drawers {
		items = append(items, item{id: d.ID, doc: d.Document})
	}

	// Sort by doc length descending (keep longest first).
	sort.Slice(items, func(i, j int) bool {
		return len(items[i].doc) > len(items[j].doc)
	})

	var keptItems []item
	var toDelete []string

	for _, it := range items {
		// Short or empty docs are auto-deleted.
		if len(it.doc) < shortDocThreshold {
			toDelete = append(toDelete, it.id)
			continue
		}

		if len(keptItems) == 0 {
			keptItems = append(keptItems, it)
			continue
		}

		// Query palace for similar docs among those already kept.
		nResults := len(keptItems)
		if nResults > 5 {
			nResults = 5
		}
		results, qErr := p.Query(it.doc, palace.QueryOptions{NResults: nResults})
		if qErr != nil {
			// On query failure, keep the drawer.
			slog.Warn("dedup: query failed, keeping drawer", "id", it.id, "err", qErr)
			keptItems = append(keptItems, it)
			continue
		}

		keptIDSet := map[string]bool{}
		for _, k := range keptItems {
			keptIDSet[k.id] = true
		}

		isDup := false
		for _, r := range results {
			if keptIDSet[r.Drawer.ID] {
				distance := 1 - r.Similarity
				if distance < threshold {
					isDup = true
					break
				}
			}
		}

		if isDup {
			toDelete = append(toDelete, it.id)
		} else {
			keptItems = append(keptItems, it)
		}
	}

	if len(toDelete) > 0 && !dryRun {
		for _, id := range toDelete {
			if dErr := p.Delete(id); dErr != nil {
				slog.Warn("dedup: delete failed", "id", id, "err", dErr)
			}
		}
	}

	kept = make([]string, len(keptItems))
	for i, k := range keptItems {
		kept[i] = k.id
	}
	return kept, toDelete, nil
}

// FindDuplicates returns duplicate groups without deleting anything.
func FindDuplicates(p *palace.Palace, threshold float64, opts DedupOptions) ([]DuplicateGroup, error) {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	minCount := opts.MinCount
	if minCount <= 0 {
		minCount = MinDrawersToCheck
	}

	groups, err := getSourceGroups(p, minCount, opts.SourcePattern, opts.Wing)
	if err != nil {
		return nil, err
	}

	var result []DuplicateGroup
	for src, ids := range groups {
		kept, deleted, err := dedupSourceGroup(p, ids, threshold, true)
		if err != nil {
			slog.Warn("dedup: source group failed", "source", src, "err", err)
			continue
		}
		result = append(result, DuplicateGroup{
			SourceFile: src,
			KeptIDs:    kept,
			DeletedIDs: deleted,
		})
	}
	return result, nil
}

// Deduplicate finds and removes duplicates, returning the count of deleted drawers.
func Deduplicate(p *palace.Palace, threshold float64, dryRun bool, opts DedupOptions) (int, error) {
	if threshold <= 0 {
		threshold = DefaultThreshold
	}
	minCount := opts.MinCount
	if minCount <= 0 {
		minCount = MinDrawersToCheck
	}

	groups, err := getSourceGroups(p, minCount, opts.SourcePattern, opts.Wing)
	if err != nil {
		return 0, err
	}

	totalDeleted := 0
	for src, ids := range groups {
		_, deleted, err := dedupSourceGroup(p, ids, threshold, dryRun)
		if err != nil {
			slog.Warn("dedup: source group failed", "source", src, "err", err)
			continue
		}
		totalDeleted += len(deleted)
	}
	return totalDeleted, nil
}

// ShowStats returns summary statistics about potential duplicates.
func ShowStats(p *palace.Palace, opts DedupOptions) (*Stats, error) {
	minCount := opts.MinCount
	if minCount <= 0 {
		minCount = MinDrawersToCheck
	}

	groups, err := getSourceGroups(p, minCount, opts.SourcePattern, opts.Wing)
	if err != nil {
		return nil, err
	}

	total := 0
	for _, ids := range groups {
		total += len(ids)
	}

	return &Stats{
		SourcesChecked: len(groups),
		TotalDrawers:   total,
	}, nil
}
