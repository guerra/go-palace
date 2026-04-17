package palace

import (
	"fmt"
	"time"

	"github.com/guerra/go-palace/pkg/halls"
)

// CompactAction selects what Compact does with selected cold drawers.
type CompactAction int

const (
	// ActionArchive moves cold drawers' hall to halls.HallArchived. The
	// drawers.hall column becomes "archived" while the drawers_vec
	// partition hall stays on the original hall — Query unconditionally
	// filters on d.hall != 'archived', so archived rows stop surfacing in
	// every semantic Query regardless of the caller's Hall filter. Get
	// bypasses the vec table so archived rows remain accessible there.
	// This is the default.
	ActionArchive CompactAction = 0
	// ActionDelete removes cold drawers outright (drawers + drawers_vec).
	ActionDelete CompactAction = 1
)

// CompactOptions configures a Compact pass. Zero-value is usable:
//
//	ColdDays        = 30
//	Action          = ActionArchive
//	ProtectedHalls  = [HallDiary, HallKnowledge]
//	DryRun          = false
//	MaxBatch        = 1000
//
// Compact falls back to filed_at when last_accessed is absent (palace
// written without TrackLastAccessed), so it works on palaces that never
// opted into recency tracking.
type CompactOptions struct {
	// ColdDays is the cutoff in days. Drawers whose last_accessed (or
	// filed_at fallback) is older than ColdDays ago are selected.
	ColdDays int
	// Action is ActionArchive or ActionDelete. Default ActionArchive.
	Action CompactAction
	// ProtectedHalls is an allowlist of halls to exclude from selection.
	// nil means "use default" ([HallDiary, HallKnowledge]). An empty
	// non-nil slice ([]string{}) means "no protection".
	ProtectedHalls []string
	// DryRun skips all mutations but still returns SelectedIDs + counts.
	DryRun bool
	// MaxBatch caps the number of selected ids. <=0 means no cap.
	MaxBatch int
}

// CompactReport summarizes a Compact pass. Archived + Deleted + Protected
// may not sum to Selected when DryRun is true (Action counters stay zero).
type CompactReport struct {
	Scanned     int
	Selected    int
	Archived    int
	Deleted     int
	Protected   int
	DryRun      bool
	SelectedIDs []string
}

// defaultProtectedHalls returns a fresh slice of halls consulted when
// opts.ProtectedHalls is nil. A non-nil empty slice on the caller side
// lets them explicitly disable protection. Returning a new slice per call
// prevents tests or callers from mutating a shared package var.
func defaultProtectedHalls() []string {
	return []string{halls.HallDiary, halls.HallKnowledge}
}

// Compact scans cold drawers and archives or deletes them per opts. A
// CompactReport is returned even on partial failure so callers can inspect
// what was done; the error (if any) ends the scan early.
func (p *Palace) Compact(opts CompactOptions) (CompactReport, error) {
	if opts.ColdDays <= 0 {
		opts.ColdDays = 30
	}
	if opts.MaxBatch <= 0 {
		opts.MaxBatch = 1000
	}
	protected := opts.ProtectedHalls
	if protected == nil {
		protected = defaultProtectedHalls()
	}

	rep := CompactReport{DryRun: opts.DryRun}

	total, err := p.Count()
	if err != nil {
		return rep, fmt.Errorf("palace: compact count: %w", err)
	}
	rep.Scanned = total

	before := time.Now().UTC().Add(-time.Duration(opts.ColdDays) * 24 * time.Hour)
	ids, err := p.ColdDrawerIDs(before, opts.MaxBatch, protected)
	if err != nil {
		return rep, fmt.Errorf("palace: compact scan: %w", err)
	}
	rep.Selected = len(ids)
	rep.SelectedIDs = ids

	if opts.DryRun || len(ids) == 0 {
		return rep, nil
	}

	const batchSize = 100
	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batch := ids[i:end]
		switch opts.Action {
		case ActionArchive:
			n, err := p.ArchiveDrawers(batch)
			if err != nil {
				return rep, fmt.Errorf("palace: compact archive: %w", err)
			}
			rep.Archived += n
		case ActionDelete:
			n, err := p.DeleteBatch(batch)
			if err != nil {
				return rep, fmt.Errorf("palace: compact delete: %w", err)
			}
			rep.Deleted += n
		default:
			return rep, fmt.Errorf("palace: compact: unknown action %d", opts.Action)
		}
	}
	return rep, nil
}
