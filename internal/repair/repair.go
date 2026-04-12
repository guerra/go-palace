// Package repair scans the palace for corrupt entries, prunes them, and
// rebuilds the vector index by re-upserting all drawers.
//
// Unlike the Python version (which deals with ChromaDB HNSW corruption),
// Go's sqlite-vec backend is more reliable. The scan still detects orphaned
// or inconsistent entries, and rebuild forces re-embedding of all drawers.
package repair

import (
	"fmt"
	"log/slog"

	"go-palace/pkg/palace"
)

// ScanResult holds the output of a palace scan.
type ScanResult struct {
	GoodIDs []string
	BadIDs  []string
	Total   int
}

// RepairResult holds the combined result of a full repair run.
type RepairResult struct {
	Scanned int
	Pruned  int
	Rebuilt int
	Errors  []error
}

// ScanPalace scans all drawers and probes each to detect corrupt entries.
// In sqlite-vec, Get always succeeds for existing IDs, so this primarily
// validates that all drawers are readable.
func ScanPalace(p *palace.Palace, wing string) (*ScanResult, error) {
	var allIDs []string
	offset := 0
	batchSize := 1000

	for {
		where := map[string]string{}
		if wing != "" {
			where["wing"] = wing
		}
		drawers, err := p.Get(palace.GetOptions{Where: where, Limit: batchSize, Offset: offset})
		if err != nil {
			return nil, fmt.Errorf("repair: scan list ids: %w", err)
		}
		if len(drawers) == 0 {
			break
		}
		for _, d := range drawers {
			allIDs = append(allIDs, d.ID)
		}
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}

	if len(allIDs) == 0 {
		return &ScanResult{Total: 0}, nil
	}

	// Probe in batches of 100.
	var goodIDs, badIDs []string
	probeBatch := 100

	for i := 0; i < len(allIDs); i += probeBatch {
		end := i + probeBatch
		if end > len(allIDs) {
			end = len(allIDs)
		}
		chunk := allIDs[i:end]

		drawers, err := p.GetByIDs(chunk)
		if err != nil {
			// Batch probe failed — fall back to per-ID.
			slog.Warn("repair: batch probe failed, falling back to per-ID", "err", err)
			for _, id := range chunk {
				single, sErr := p.GetByIDs([]string{id})
				if sErr != nil || len(single) == 0 {
					badIDs = append(badIDs, id)
				} else {
					goodIDs = append(goodIDs, id)
				}
			}
			continue
		}

		gotSet := map[string]bool{}
		for _, d := range drawers {
			gotSet[d.ID] = true
		}
		for _, id := range chunk {
			if gotSet[id] {
				goodIDs = append(goodIDs, id)
			} else {
				badIDs = append(badIDs, id)
			}
		}
	}

	return &ScanResult{
		GoodIDs: goodIDs,
		BadIDs:  badIDs,
		Total:   len(allIDs),
	}, nil
}

// PruneCorrupt deletes the specified bad IDs from the palace.
func PruneCorrupt(p *palace.Palace, badIDs []string, dryRun bool) (int, error) {
	if len(badIDs) == 0 {
		return 0, nil
	}
	if dryRun {
		return len(badIDs), nil
	}

	deleted := 0
	var lastErr error
	for _, id := range badIDs {
		if err := p.Delete(id); err != nil {
			slog.Warn("repair: delete failed", "id", id, "err", err)
			lastErr = err
		} else {
			deleted++
		}
	}
	if lastErr != nil && deleted == 0 {
		return 0, fmt.Errorf("repair: prune failed: %w", lastErr)
	}
	return deleted, nil
}

// RebuildIndex reads all drawers and re-upserts them to force re-embedding.
func RebuildIndex(p *palace.Palace, dryRun bool) (*RepairResult, error) {
	total, err := p.Count()
	if err != nil {
		return nil, fmt.Errorf("repair: count: %w", err)
	}

	result := &RepairResult{Scanned: total}

	if total == 0 {
		return result, nil
	}

	if dryRun {
		result.Rebuilt = total
		return result, nil
	}

	offset := 0
	batchSize := 100
	rebuilt := 0

	for {
		drawers, err := p.Get(palace.GetOptions{Limit: batchSize, Offset: offset})
		if err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("read at offset %d: %w", offset, err))
			break
		}
		if len(drawers) == 0 {
			break
		}
		if err := p.UpsertBatch(drawers); err != nil {
			result.Errors = append(result.Errors, fmt.Errorf("upsert at offset %d: %w", offset, err))
			break
		}
		rebuilt += len(drawers)
		offset += len(drawers)
		if len(drawers) < batchSize {
			break
		}
	}

	result.Rebuilt = rebuilt
	return result, nil
}

// Repair runs the full repair pipeline: scan, prune bad IDs, rebuild index.
func Repair(p *palace.Palace, dryRun bool) (*RepairResult, error) {
	scan, err := ScanPalace(p, "")
	if err != nil {
		return nil, fmt.Errorf("repair: scan: %w", err)
	}

	result := &RepairResult{Scanned: scan.Total}

	// Prune corrupt entries.
	if len(scan.BadIDs) > 0 {
		pruned, err := PruneCorrupt(p, scan.BadIDs, dryRun)
		if err != nil {
			result.Errors = append(result.Errors, err)
		}
		result.Pruned = pruned
	}

	// Rebuild index.
	rebuild, err := RebuildIndex(p, dryRun)
	if err != nil {
		result.Errors = append(result.Errors, err)
	} else {
		result.Rebuilt = rebuild.Rebuilt
		result.Errors = append(result.Errors, rebuild.Errors...)
	}

	return result, nil
}
