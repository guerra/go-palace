package palace_test

import (
	"testing"
	"time"

	"github.com/guerra/go-palace/pkg/halls"
	"github.com/guerra/go-palace/pkg/palace"
)

// seedWithAge inserts n drawers in hall with filed_at = age ago.
func seedWithAge(t *testing.T, p *palace.Palace, hall string, src string, n int, age time.Duration) []string {
	t.Helper()
	var ds []palace.Drawer
	for i := 0; i < n; i++ {
		d := makeDrawer("w", hall, "r", src, i, "content")
		d.FiledAt = time.Now().Add(-age)
		ds = append(ds, d)
	}
	if err := p.UpsertBatch(ds); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	ids := make([]string, n)
	for i, d := range ds {
		ids[i] = d.ID
	}
	return ids
}

func TestCompactSelectsColdDrawers(t *testing.T) {
	p := openTest(t)
	coldIDs := seedWithAge(t, p, halls.HallConversations, "cold.md", 5, 60*24*time.Hour)
	_ = seedWithAge(t, p, halls.HallConversations, "warm.md", 5, 1*time.Hour)

	rep, err := p.Compact(palace.CompactOptions{
		ColdDays: 30,
		Action:   palace.ActionArchive,
		// Conversations must NOT be default-protected here.
		ProtectedHalls: []string{halls.HallDiary},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 5 {
		t.Errorf("Selected = %d; want 5", rep.Selected)
	}
	if rep.Archived != 5 {
		t.Errorf("Archived = %d; want 5", rep.Archived)
	}
	// Every cold id appears in SelectedIDs.
	seen := map[string]bool{}
	for _, id := range rep.SelectedIDs {
		seen[id] = true
	}
	for _, id := range coldIDs {
		if !seen[id] {
			t.Errorf("cold id %s missing from SelectedIDs", id)
		}
	}
}

func TestCompactActionArchive_MovesHall(t *testing.T) {
	p := openTest(t)
	_ = seedWithAge(t, p, halls.HallConversations, "cold.md", 3, 60*24*time.Hour)

	_, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	archived, err := p.Get(palace.GetOptions{Where: map[string]string{"hall": halls.HallArchived}})
	if err != nil {
		t.Fatalf("get archived: %v", err)
	}
	if len(archived) != 3 {
		t.Errorf("archived count = %d; want 3", len(archived))
	}
}

func TestCompactActionDelete_Removes(t *testing.T) {
	p := openTest(t)
	_ = seedWithAge(t, p, halls.HallConversations, "cold.md", 3, 60*24*time.Hour)

	rep, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionDelete,
		ProtectedHalls: []string{},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Deleted != 3 {
		t.Errorf("Deleted = %d; want 3", rep.Deleted)
	}
	n, err := p.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 0 {
		t.Errorf("count after delete = %d; want 0", n)
	}
}

func TestCompactDryRun(t *testing.T) {
	p := openTest(t)
	_ = seedWithAge(t, p, halls.HallConversations, "cold.md", 4, 60*24*time.Hour)
	rep, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{},
		DryRun:         true,
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 4 {
		t.Errorf("Selected = %d; want 4", rep.Selected)
	}
	if rep.Archived != 0 || rep.Deleted != 0 {
		t.Errorf("DryRun should not mutate: archived=%d deleted=%d", rep.Archived, rep.Deleted)
	}
	// Palace state must be untouched.
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"hall": halls.HallArchived}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("DryRun mutated palace: %d archived", len(got))
	}
}

func TestCompactProtectedHalls(t *testing.T) {
	p := openTest(t)
	_ = seedWithAge(t, p, halls.HallDiary, "diary.md", 5, 60*24*time.Hour)
	_ = seedWithAge(t, p, halls.HallConversations, "conv.md", 5, 60*24*time.Hour)

	rep, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{halls.HallDiary},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 5 {
		t.Errorf("Selected = %d; want 5 (only conversations, diary protected)", rep.Selected)
	}
	// Diary drawers still unarchived.
	diary, err := p.Get(palace.GetOptions{Where: map[string]string{"hall": halls.HallDiary}})
	if err != nil {
		t.Fatalf("get diary: %v", err)
	}
	if len(diary) != 5 {
		t.Errorf("diary remaining = %d; want 5", len(diary))
	}
}

func TestCompactFallbackToFiledAt(t *testing.T) {
	p := openTest(t)
	// filed_at 60d ago, no last_accessed key — must be selected as cold.
	_ = seedWithAge(t, p, halls.HallConversations, "aged.md", 1, 60*24*time.Hour)
	rep, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{},
	})
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 1 {
		t.Errorf("Selected = %d; want 1 (filed_at fallback)", rep.Selected)
	}
}

func TestCompactDefaultProtectsDiaryAndKnowledge(t *testing.T) {
	p := openTest(t)
	_ = seedWithAge(t, p, halls.HallDiary, "d.md", 2, 60*24*time.Hour)
	_ = seedWithAge(t, p, halls.HallKnowledge, "k.md", 2, 60*24*time.Hour)
	_ = seedWithAge(t, p, halls.HallConversations, "c.md", 2, 60*24*time.Hour)

	rep, err := p.Compact(palace.CompactOptions{ColdDays: 30}) // zero-value → defaults
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if rep.Selected != 2 {
		t.Errorf("Selected = %d; want 2 (diary+knowledge default-protected)", rep.Selected)
	}
}

// TestCompactArchive_ExcludesArchivedFromQuery is the gp-5 review H1
// regression guard: after Compact(ArchiveAction) moves cold drawers to
// hall='archived', a subsequent unfiltered semantic Query MUST NOT
// return them. Before the fix, Query joined drawers_vec on drawers but
// only filtered on v.hall (never d.hall), so archived rows leaked into
// every unfiltered search with Drawer.Hall='archived' in the result.
func TestCompactArchive_ExcludesArchivedFromQuery(t *testing.T) {
	p := openTest(t)
	coldIDs := seedWithAge(t, p, halls.HallConversations, "cold.md", 3, 60*24*time.Hour)
	_ = seedWithAge(t, p, halls.HallConversations, "warm.md", 2, 1*time.Hour)

	if _, err := p.Compact(palace.CompactOptions{
		ColdDays:       30,
		Action:         palace.ActionArchive,
		ProtectedHalls: []string{},
	}); err != nil {
		t.Fatalf("compact: %v", err)
	}
	// Archived drawers are still visible via Get.
	archived, err := p.Get(palace.GetOptions{Where: map[string]string{"hall": halls.HallArchived}})
	if err != nil {
		t.Fatalf("get archived: %v", err)
	}
	if len(archived) != 3 {
		t.Fatalf("archived via Get = %d; want 3", len(archived))
	}

	// Unfiltered semantic Query MUST skip archived rows.
	res, err := p.Query("content", palace.QueryOptions{NResults: 50})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	archivedSet := map[string]bool{}
	for _, id := range coldIDs {
		archivedSet[id] = true
	}
	for _, r := range res {
		if r.Drawer.Hall == halls.HallArchived {
			t.Errorf("query returned drawer with Hall='archived': id=%s", r.Drawer.ID)
		}
		if archivedSet[r.Drawer.ID] {
			t.Errorf("query returned archived drawer id %s (must be excluded)", r.Drawer.ID)
		}
	}
	// And with an explicit Hall=HallArchived filter, Query returns nothing
	// (v.hall never holds 'archived', so the partition filter is empty by
	// construction). This is the documented contract — archived rows are
	// Get-only.
	res2, err := p.Query("content", palace.QueryOptions{
		Hall:     halls.HallArchived,
		NResults: 50,
	})
	if err != nil {
		t.Fatalf("query archived filter: %v", err)
	}
	if len(res2) != 0 {
		t.Errorf("Query(Hall=archived) returned %d; want 0 (archived rows are Get-only)", len(res2))
	}
}
