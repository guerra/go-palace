package repair

import (
	"fmt"

	"github.com/guerra/go-palace/pkg/palace"
)

// RepairMode selects whether Repair mutates the palace.
type RepairMode int

const (
	// ModeReportOnly runs every check but leaves the palace untouched.
	// Default.
	ModeReportOnly RepairMode = 0
	// ModeAutoDelete removes detected orphans (and ONLY orphans). WAL
	// integrity issues and dim mismatches are never auto-fixed — those
	// need a human.
	ModeAutoDelete RepairMode = 1
)

// RepairOptions configures a Repair pass. Zero-value is a full
// report-only audit.
type RepairOptions struct {
	Mode RepairMode
	// SkipIntegrityCheck skips PRAGMA integrity_check entirely. Useful
	// for routine health probes on large palaces where the full scan is
	// too slow.
	SkipIntegrityCheck bool
	// QuickCheck, when true (and SkipIntegrityCheck is false), runs
	// PRAGMA quick_check instead of integrity_check. Faster; skips some
	// constraint passes.
	QuickCheck bool
}

// DimInfo reports the stored vs. embedder dimensions when they diverge.
// Surfaces only inside RepairReport.DimMismatch (nil when matched).
type DimInfo struct {
	Stored   int
	Embedder int
}

// RepairReport summarizes a Repair pass. Empty slices / zero counts / nil
// DimMismatch mean "clean".
type RepairReport struct {
	IntegrityIssues []string
	DrawerOrphans   []string
	VecOrphans      []string
	DimMismatch     *DimInfo
	OrphansDeleted  int
	Mode            RepairMode
}

// Repair runs the audit checks against p and returns a RepairReport. Errors
// from individual checks are wrapped and returned immediately — a partial
// report is surfaced alongside the error so callers can inspect what ran.
func Repair(p *palace.Palace, opts RepairOptions) (RepairReport, error) {
	rep := RepairReport{Mode: opts.Mode}

	if !opts.SkipIntegrityCheck {
		var (
			issues []string
			err    error
		)
		if opts.QuickCheck {
			issues, err = p.QuickCheck()
		} else {
			issues, err = p.IntegrityCheck()
		}
		if err != nil {
			return rep, fmt.Errorf("repair: integrity: %w", err)
		}
		rep.IntegrityIssues = issues
	}

	drawerOrphans, vecOrphans, err := p.ScanOrphans()
	if err != nil {
		return rep, fmt.Errorf("repair: scan orphans: %w", err)
	}
	rep.DrawerOrphans = drawerOrphans
	rep.VecOrphans = vecOrphans

	stored, err := p.EmbeddingDim()
	if err != nil {
		return rep, fmt.Errorf("repair: embedding_dim: %w", err)
	}
	probed := p.ProbeEmbedderDim()
	if stored > 0 && stored != probed {
		rep.DimMismatch = &DimInfo{Stored: stored, Embedder: probed}
	}

	if opts.Mode == ModeAutoDelete && (len(drawerOrphans) > 0 || len(vecOrphans) > 0) {
		n, err := p.DeleteOrphans(drawerOrphans, vecOrphans)
		if err != nil {
			return rep, fmt.Errorf("repair: delete orphans: %w", err)
		}
		rep.OrphansDeleted = n
	}

	return rep, nil
}
