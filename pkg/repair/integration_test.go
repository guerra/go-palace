package repair_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/kg"
	"github.com/guerra/go-palace/pkg/palace"
	"github.com/guerra/go-palace/pkg/repair"
)

// TestIntegration_ExtractCompactRepair wires the full gp-5 pipeline and
// asserts end-to-end behavior:
//
//  1. Open palace with AutoExtractKG + TrackLastAccessed.
//  2. Upsert 10 drawers with verb-phrase prose — triples land in KG.
//  3. Query 5 — those 5 bump last_accessed.
//  4. Raw-SQL bump the other 5 back 60 days.
//  5. Compact → 5 archived.
//  6. Repair → clean.
func TestIntegration_ExtractCompactRepair(t *testing.T) {
	tmp := t.TempDir()
	palacePath := filepath.Join(tmp, "palace.db")
	kgPath := filepath.Join(tmp, "kg.db")

	// Build kg + adapter.
	k, err := kg.Open(kgPath)
	if err != nil {
		t.Fatalf("kg open: %v", err)
	}
	t.Cleanup(func() { _ = k.Close() })
	sink := kg.NewPalaceAdapter(k)
	detector := kg.NewStatelessEntityDetector()

	// Open palace wired with extract + track.
	opts := palace.DefaultPalaceOptions()
	opts.AutoExtractKG = true
	opts.KG = sink
	opts.EntityRegistry = detector
	opts.AutoExtractFn = kg.AutoExtractTriples
	opts.TrackLastAccessed = true

	p, err := palace.OpenWithOptions(palacePath, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim), opts)
	if err != nil {
		t.Fatalf("palace open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// Construct 10 drawers: the first 6 carry dialogue/addressed signals so
	// entity.Detect upgrades the proper nouns to person/project/uncertain —
	// AutoExtractTriples then anchors verb phrases against those entities.
	// The remaining 4 are noise (no verb-phrase matches).
	docs := []string{
		"Alice said she works at Acme. Hey Alice, thanks Alice for joining.",
		"Bob said he lives in Berlin these days. Hey Bob, thanks Bob.",
		"Carol said she uses Python every morning. Hey Carol, thanks Carol.",
		"Dan said he prefers Emacs always. Hey Dan, thanks Dan for helping.",
		"Eve said she started MemPalace yesterday. Hey Eve, thanks Eve.",
		"Frank said he finished MemPalace finally. Hey Frank, thanks Frank.",
		"Generic conversation about nothing specific here.",
		"Random chat content without any verb-anchored entities.",
		"Team discussion went long today.",
		"Quick note, nothing of substance.",
	}
	drawers := make([]palace.Drawer, 0, 10)
	for i, doc := range docs {
		drawers = append(drawers, palace.Drawer{
			ID:         palace.ComputeDrawerID("w", "r", "f.md", i),
			Document:   doc,
			Wing:       "w",
			Hall:       halls.HallConversations,
			Room:       "r",
			SourceFile: "f.md",
			ChunkIndex: i,
			AddedBy:    "test",
			FiledAt:    time.Now(),
		})
	}
	if err := p.UpsertBatch(drawers); err != nil {
		t.Fatalf("upsert batch: %v", err)
	}

	stats, err := k.Stats()
	if err != nil {
		t.Fatalf("kg stats: %v", err)
	}
	if stats.Triples < 6 {
		t.Errorf("kg triples = %d; want at least 6 (one per verb phrase)", stats.Triples)
	}

	// Query something that hits the first 5 drawers. Since FakeEmbedder is
	// deterministic on text, we run 5 queries to guarantee TrackLastAccessed
	// fires on 5 distinct ids.
	queryIDs := make(map[string]bool)
	for i := 0; i < 5; i++ {
		res, err := p.Query(drawers[i].Document, palace.QueryOptions{NResults: 1})
		if err != nil {
			t.Fatalf("query %d: %v", i, err)
		}
		if len(res) == 0 {
			t.Fatalf("query %d: no results", i)
		}
		queryIDs[res[0].Drawer.ID] = true
	}
	if len(queryIDs) < 5 {
		t.Fatalf("expected 5 distinct queried ids, got %d", len(queryIDs))
	}

	// Raw-SQL: push the NOT-queried 5 drawers back 60 days on last_accessed.
	raw, err := sql.Open("sqlite3", palacePath)
	if err != nil {
		t.Fatalf("raw open: %v", err)
	}
	oldTS := time.Now().UTC().Add(-60 * 24 * time.Hour).Format(time.RFC3339Nano)
	for _, d := range drawers {
		if queryIDs[d.ID] {
			continue
		}
		if _, err := raw.Exec(
			`UPDATE drawers
			 SET metadata_json = json_set(metadata_json, '$.last_accessed', ?)
			 WHERE id = ?`, oldTS, d.ID); err != nil {
			t.Fatalf("raw update %s: %v", d.ID, err)
		}
	}
	_ = raw.Close()

	// Compact: only the 5 non-queried (cold) drawers should be selected.
	rep, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 5 {
		t.Errorf("compact Selected = %d; want 5", rep.Selected)
	}
	if rep.Archived != 5 {
		t.Errorf("compact Archived = %d; want 5", rep.Archived)
	}

	// Repair: clean.
	rr, err := repair.Repair(p, repair.RepairOptions{})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if len(rr.IntegrityIssues) != 0 {
		t.Errorf("integrity issues = %v; want none", rr.IntegrityIssues)
	}
	if len(rr.DrawerOrphans) != 0 || len(rr.VecOrphans) != 0 {
		t.Errorf("orphans = (%v, %v); want clean", rr.DrawerOrphans, rr.VecOrphans)
	}
	if rr.DimMismatch != nil {
		t.Errorf("DimMismatch = %+v; want nil", rr.DimMismatch)
	}
}
