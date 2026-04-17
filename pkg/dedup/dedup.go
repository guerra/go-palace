// Package dedup detects near-duplicate drawers (cosine similarity >=
// threshold) within a (wing, hall, source_file) partition and either
// deletes the losers or merges their metadata into the longest kept
// drawer. Runs through the palace.Palace API only — no sqlite / vec
// imports here. See pkg/palace.MergeAndDelete for the atomic primitive.
//
// FakeEmbedder note: tests using pkg/embed.FakeEmbedder see ~0.75 cosine
// similarity between random inputs (the fake produces all-positive
// [0,1] vectors with no locality). Thresholds below ~0.8 will collapse
// unrelated drawers under FakeEmbedder — not a dedup bug but a
// property of the fake. Default 0.95 is robust.
package dedup

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/guerra/go-palace/pkg/palace"
)

// Tunables.
const (
	// DefaultThreshold is cosine SIMILARITY in (0, 1]. Higher = stricter.
	// This is a BREAKING unit change from internal/dedup's cosine distance.
	DefaultThreshold = 0.95
	// MinDrawersToCheck skips small groups where dedup is not worth the
	// query overhead.
	MinDrawersToCheck = 5
	// shortDocThreshold auto-drops drawers with documents shorter than
	// this many runes (kept from internal/dedup for parity).
	shortDocThreshold = 20
)

// ErrInvalidThreshold is returned when DedupOptions.Threshold is outside (0, 1].
var ErrInvalidThreshold = errors.New("dedup: threshold must be in (0, 1]")

// DedupOptions controls how dedup scopes its work.
type DedupOptions struct {
	// Threshold is cosine similarity in (0, 1]. Zero-value is replaced by
	// DefaultThreshold. A value > 1 or < 0 returns ErrInvalidThreshold.
	Threshold float64
	// Wing, if non-empty, restricts dedup to a single wing.
	Wing string
	// Hall, if non-empty, restricts dedup to a single hall. Cross-hall
	// drawers are never merged regardless of Hall: the partition key is
	// always (wing, hall, source_file).
	Hall string
	// SourcePattern is a case-insensitive substring filter on source_file.
	SourcePattern string
	// MinCount skips groups with fewer drawers. Zero-value uses MinDrawersToCheck.
	MinCount int
	// Merge, when true, merges each loser's metadata into the kept winner
	// via palace.MergeAndDelete. When false, losers are simply deleted.
	Merge bool
	// DryRun, when true, computes the plan and populates the report but
	// makes no writes.
	DryRun bool
}

// DuplicateGroup records what happened for one partition group.
type DuplicateGroup struct {
	SourceFile string
	Hall       string
	KeptID     string
	MergedIDs  []string // losers whose metadata was merged into the winner
	DroppedIDs []string // losers deleted without metadata merge
}

// DedupReport summarizes a dedup run.
type DedupReport struct {
	SourcesChecked int
	Kept           int
	Merged         int
	Dropped        int
	Groups         []DuplicateGroup
}

// Run executes the dedup pass.
func Run(p *palace.Palace, opts DedupOptions) (DedupReport, error) {
	if opts.Threshold < 0 || opts.Threshold > 1 {
		return DedupReport{}, ErrInvalidThreshold
	}
	if opts.Threshold == 0 {
		opts.Threshold = DefaultThreshold
	}
	minCount := opts.MinCount
	if minCount <= 0 {
		minCount = MinDrawersToCheck
	}

	groups, err := getSourceGroups(p, minCount, opts.SourcePattern, opts.Wing, opts.Hall)
	if err != nil {
		return DedupReport{}, err
	}

	report := DedupReport{SourcesChecked: len(groups)}
	for key, ids := range groups {
		wing, hall, src := splitGroupKey(key)
		gResult, err := dedupSourceGroup(p, ids, wing, hall, src, opts)
		if err != nil {
			slog.Warn("dedup: source group failed",
				"wing", wing, "hall", hall, "source", src, "err", err)
			continue
		}
		if gResult != nil {
			report.Groups = append(report.Groups, *gResult)
			report.Kept += 1
			report.Merged += len(gResult.MergedIDs)
			report.Dropped += len(gResult.DroppedIDs)
		}
	}
	return report, nil
}

// groupKey combines wing, hall, and source into a stable map key matching the
// (wing, hall, source_file) partition that MergeAndDelete enforces.
func groupKey(wing, hall, src string) string {
	return wing + "|" + hall + "|" + src
}

// splitGroupKey reverses groupKey.
func splitGroupKey(key string) (wing, hall, src string) {
	wing, rest, _ := strings.Cut(key, "|")
	hall, src, _ = strings.Cut(rest, "|")
	return wing, hall, src
}

// getSourceGroups paginates the palace and groups drawer IDs by
// (hall, source_file), returning only groups with at least minCount entries.
func getSourceGroups(
	p *palace.Palace, minCount int, sourcePattern, wing, hall string,
) (map[string][]string, error) {
	groups := map[string][]string{}
	offset := 0
	batchSize := 1000
	for {
		where := map[string]string{}
		if wing != "" {
			where["wing"] = wing
		}
		if hall != "" {
			where["hall"] = hall
		}
		drawers, err := p.Get(palace.GetOptions{Where: where, Limit: batchSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("dedup: get source groups: %w", err)
		}
		if len(drawers) == 0 {
			break
		}
		lowerPattern := ""
		if sourcePattern != "" {
			lowerPattern = strings.ToLower(sourcePattern)
		}
		for _, d := range drawers {
			src := d.SourceFile
			if src == "" {
				src = "unknown"
			}
			if lowerPattern != "" && !strings.Contains(strings.ToLower(src), lowerPattern) {
				continue
			}
			k := groupKey(d.Wing, d.Hall, src)
			groups[k] = append(groups[k], d.ID)
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}
	out := map[string][]string{}
	for k, ids := range groups {
		if len(ids) >= minCount {
			out[k] = ids
		}
	}
	return out, nil
}

// dedupSourceGroup runs greedy longest-first dedup within one partition.
// Returns nil if nothing to merge/drop (all drawers kept and no shorts).
func dedupSourceGroup(
	p *palace.Palace,
	drawerIDs []string,
	wing, hall, src string,
	opts DedupOptions,
) (*DuplicateGroup, error) {
	drawers, err := p.GetByIDs(drawerIDs)
	if err != nil {
		return nil, fmt.Errorf("dedup: get drawers: %w", err)
	}

	type item struct {
		id   string
		doc  string
		meta map[string]any
	}
	items := make([]item, 0, len(drawers))
	for _, d := range drawers {
		items = append(items, item{id: d.ID, doc: d.Document, meta: d.Metadata})
	}
	// Longest-first. Stable with ID tiebreak so repeat dedup runs pick the
	// same winner for equal-length drawers — audits and --merge metadata
	// attribution must be reproducible.
	sort.SliceStable(items, func(i, j int) bool {
		if len(items[i].doc) != len(items[j].doc) {
			return len(items[i].doc) > len(items[j].doc)
		}
		return items[i].id < items[j].id
	})

	var (
		keptItems []item
		dropped   []string // short-doc drops (never merge — no signal)
		dupLosers []item   // similarity-matched losers (eligible for merge)
	)

	for _, it := range items {
		if len(it.doc) < shortDocThreshold {
			dropped = append(dropped, it.id)
			continue
		}
		if len(keptItems) == 0 {
			keptItems = append(keptItems, it)
			continue
		}
		// Query against kept set via palace semantic search. Use a
		// generous NResults so the winner is visible among top matches
		// even when many identical vectors share the same distance and
		// SQLite returns them in an indeterminate order. Scope by the
		// full partition tuple (wing, hall) — cross-wing matches cannot
		// be valid dedup candidates and only waste query work.
		nResults := len(keptItems) + 5
		if nResults > 50 {
			nResults = 50
		}
		results, qErr := p.Query(it.doc, palace.QueryOptions{
			NResults: nResults, Wing: wing, Hall: hall,
		})
		if qErr != nil {
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
			if keptIDSet[r.Drawer.ID] && r.Similarity >= opts.Threshold {
				isDup = true
				break
			}
		}
		if isDup {
			dupLosers = append(dupLosers, it)
		} else {
			keptItems = append(keptItems, it)
		}
	}

	if len(keptItems) == 0 {
		// All drawers were below shortDocThreshold. Nothing to keep.
		if len(dropped) == 0 {
			return nil, nil
		}
		// Delete the short drops if not DryRun. No winner to merge into.
		if !opts.DryRun {
			for _, id := range dropped {
				if dErr := p.Delete(id); dErr != nil {
					slog.Warn("dedup: delete failed", "id", id, "err", dErr)
				}
			}
		}
		return &DuplicateGroup{
			SourceFile: src,
			Hall:       hall,
			KeptID:     "",
			DroppedIDs: dropped,
		}, nil
	}

	winner := keptItems[0]
	// Any additional kept items beyond the winner are ALSO kept — they
	// represent distinct (non-duplicate) drawers inside the same partition.
	// Only dupLosers + short-drops are removed.
	group := &DuplicateGroup{
		SourceFile: src,
		Hall:       hall,
		KeptID:     winner.id,
	}

	// Perform writes unless DryRun.
	if opts.DryRun {
		// Populate MergedIDs / DroppedIDs report without actually writing.
		group.DroppedIDs = append(group.DroppedIDs, dropped...)
		if opts.Merge {
			for _, l := range dupLosers {
				group.MergedIDs = append(group.MergedIDs, l.id)
			}
		} else {
			for _, l := range dupLosers {
				group.DroppedIDs = append(group.DroppedIDs, l.id)
			}
		}
		return group, nil
	}

	if opts.Merge && (len(dupLosers) > 0 || len(dropped) > 0) {
		// Atomic: merge loser metadata into winner AND remove both
		// dupLosers and short-doc drops in a single transaction. Short
		// docs contribute no metadata but are safely included as plain
		// deletes under the same partition guard.
		merged := map[string]any{}
		mergedIDs := make([]string, 0, len(dupLosers))
		allLoserIDs := make([]string, 0, len(dupLosers)+len(dropped))
		for _, l := range dupLosers {
			mergedIDs = append(mergedIDs, l.id)
			allLoserIDs = append(allLoserIDs, l.id)
			for k, v := range l.meta {
				merged[k] = v
			}
		}
		allLoserIDs = append(allLoserIDs, dropped...)
		if err := p.MergeAndDelete(winner.id, allLoserIDs, merged); err != nil {
			return nil, fmt.Errorf("dedup: merge and delete: %w", err)
		}
		group.MergedIDs = mergedIDs
		group.DroppedIDs = append(group.DroppedIDs, dropped...)
	} else {
		for _, l := range dupLosers {
			if dErr := p.Delete(l.id); dErr != nil {
				slog.Warn("dedup: delete failed", "id", l.id, "err", dErr)
				continue
			}
			group.DroppedIDs = append(group.DroppedIDs, l.id)
		}
		// Short-doc drops use plain Delete when Merge is off.
		for _, id := range dropped {
			if dErr := p.Delete(id); dErr != nil {
				slog.Warn("dedup: delete failed", "id", id, "err", dErr)
				continue
			}
			group.DroppedIDs = append(group.DroppedIDs, id)
		}
	}
	return group, nil
}
