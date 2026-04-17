package palace_test

import (
	"errors"
	"testing"

	"github.com/guerra/go-palace/pkg/palace"
)

func TestMergeAndDelete_HappyPath(t *testing.T) {
	p := openTest(t)
	winner := makeDrawer("w", "knowledge", "r", "a.md", 0, "winner content long enough")
	winner.Metadata = map[string]any{"tag": "w"}
	loser1 := makeDrawer("w", "knowledge", "r", "a.md", 1, "loser one content")
	loser1.Metadata = map[string]any{"l1": "x"}
	loser2 := makeDrawer("w", "knowledge", "r", "a.md", 2, "loser two content")
	loser2.Metadata = map[string]any{"l2": "y"}
	if err := p.UpsertBatch([]palace.Drawer{winner, loser1, loser2}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	mergedMeta := map[string]any{"l1": "x", "l2": "y"}
	if err := p.MergeAndDelete(winner.ID, []string{loser1.ID, loser2.ID}, mergedMeta); err != nil {
		t.Fatalf("MergeAndDelete: %v", err)
	}

	// Winner still present with merged metadata.
	got, err := p.GetByIDs([]string{winner.ID})
	if err != nil || len(got) != 1 {
		t.Fatalf("winner fetch: err=%v len=%d", err, len(got))
	}
	if got[0].Metadata["tag"] != "w" || got[0].Metadata["l1"] != "x" || got[0].Metadata["l2"] != "y" {
		t.Errorf("metadata merge incomplete: %v", got[0].Metadata)
	}

	// Losers gone from drawers.
	gone, err := p.GetByIDs([]string{loser1.ID, loser2.ID})
	if err != nil {
		t.Fatalf("loser fetch: %v", err)
	}
	if len(gone) != 0 {
		t.Errorf("losers still present: %d rows", len(gone))
	}

	// Losers also removed from vec table — Query should not return them.
	res, err := p.Query("loser one content", palace.QueryOptions{NResults: 10})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	for _, r := range res {
		if r.Drawer.ID == loser1.ID || r.Drawer.ID == loser2.ID {
			t.Errorf("loser %s still in drawers_vec", r.Drawer.ID)
		}
	}
}

func TestMergeAndDelete_CrossPartition(t *testing.T) {
	p := openTest(t)
	winner := makeDrawer("w", "knowledge", "r", "a.md", 0, "winner content long enough")
	winner.Metadata = map[string]any{"tag": "w"}
	// Different hall -> cross-partition.
	loser := makeDrawer("w", "diary", "r", "a.md", 1, "loser content here")
	loser.Metadata = map[string]any{"l": "x"}
	if err := p.UpsertBatch([]palace.Drawer{winner, loser}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	err := p.MergeAndDelete(winner.ID, []string{loser.ID}, map[string]any{"new": "data"})
	if !errors.Is(err, palace.ErrDedupCrossPartition) {
		t.Fatalf("err = %v, want ErrDedupCrossPartition", err)
	}

	// Rollback: both still present with ORIGINAL metadata.
	got, err := p.GetByIDs([]string{winner.ID, loser.ID})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("rollback failed: expected 2 rows, got %d", len(got))
	}
	for _, d := range got {
		if d.ID == winner.ID {
			if d.Metadata["tag"] != "w" || d.Metadata["new"] != nil {
				t.Errorf("winner metadata was modified despite rollback: %v", d.Metadata)
			}
		}
	}
}

func TestMergeAndDelete_UnknownWinner(t *testing.T) {
	p := openTest(t)
	loser := makeDrawer("w", "knowledge", "r", "a.md", 1, "loser content")
	if err := p.Upsert(loser); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	err := p.MergeAndDelete("drawer_w_r_nonexistent000000000000", []string{loser.ID}, nil)
	if !errors.Is(err, palace.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
	// Loser untouched.
	got, _ := p.GetByIDs([]string{loser.ID})
	if len(got) != 1 {
		t.Errorf("loser should still exist, got %d rows", len(got))
	}
}

func TestMergeAndDelete_EmptyLosers(t *testing.T) {
	p := openTest(t)
	winner := makeDrawer("w", "knowledge", "r", "a.md", 0, "solo winner content")
	winner.Metadata = map[string]any{"tag": "w"}
	if err := p.Upsert(winner); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if err := p.MergeAndDelete(winner.ID, nil, nil); err != nil {
		t.Errorf("empty merge should be no-op, got err=%v", err)
	}
	got, _ := p.GetByIDs([]string{winner.ID})
	if len(got) != 1 {
		t.Fatalf("winner lost: %d rows", len(got))
	}
	if got[0].Metadata["tag"] != "w" {
		t.Errorf("winner metadata changed: %v", got[0].Metadata)
	}
}

func TestMergeAndDelete_EmptyWinnerID(t *testing.T) {
	p := openTest(t)
	err := p.MergeAndDelete("", nil, nil)
	if !errors.Is(err, palace.ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}
