// Package halls defines the hall taxonomy — the semantic bucket a drawer
// belongs to within a wing/room. Halls are the 4th tier of the memory palace
// (wing > hall > room > drawer).
//
// The stored value is a bare word — no "hall_" prefix — because the COLUMN is
// already named hall, so "hall_diary" as a value is redundant. Legacy palaces
// may have written "hall_diary" into drawer metadata; Detect strips that
// prefix when reading metadata so migrations backfill the bare form.
package halls

import "strings"

// The six canonical halls. Stored values are bare words (no "hall_" prefix).
const (
	HallConversations = "conversations"
	HallJournal       = "journal"
	HallDiary         = "diary"
	HallKnowledge     = "knowledge"
	HallTasks         = "tasks"
	HallScratch       = "scratch"
)

// HallArchived is a transitional hall value used by Compact to mark cold
// drawers. It is intentionally NOT part of All and IsValid returns false for
// it — archive is a lifecycle state, not a canonical classification.
const HallArchived = "archived"

// All lists every valid hall, in canonical order.
var All = []string{
	HallConversations,
	HallJournal,
	HallDiary,
	HallKnowledge,
	HallTasks,
	HallScratch,
}

// valid is the membership set built from All.
var valid = func() map[string]struct{} {
	m := make(map[string]struct{}, len(All))
	for _, h := range All {
		m[h] = struct{}{}
	}
	return m
}()

// IsValid reports whether h is one of the six canonical halls.
func IsValid(h string) bool {
	_, ok := valid[h]
	return ok
}

// Detect classifies a drawer into a hall using deterministic heuristics.
// Priority order (first match wins):
//  1. metadata["hall"] is a valid hall, or legacy "hall_<name>" form
//  2. metadata["ingest_mode"] == "convos" → conversations
//  3. room == "diary" or addedBy starts with "diary_" → diary
//  4. room == "tasks" or "todo" → tasks
//  5. room == "scratch" or "wip" → scratch
//  6. room == "journal" → journal
//  7. default → knowledge
//
// content is accepted for future extension (content-based classifiers); the
// current heuristic does not use it.
func Detect(content, room, addedBy string, metadata map[string]any) string {
	_ = content // reserved for future content-based rules

	if metadata != nil {
		if raw, ok := metadata["hall"].(string); ok && raw != "" {
			// Strip legacy "hall_" prefix (e.g. "hall_diary" → "diary").
			bare := strings.TrimPrefix(raw, "hall_")
			if IsValid(bare) {
				return bare
			}
		}
		if mode, ok := metadata["ingest_mode"].(string); ok && mode == "convos" {
			return HallConversations
		}
	}

	if room == "diary" || strings.HasPrefix(addedBy, "diary_") {
		return HallDiary
	}
	if room == "tasks" || room == "todo" {
		return HallTasks
	}
	if room == "scratch" || room == "wip" {
		return HallScratch
	}
	if room == "journal" {
		return HallJournal
	}
	return HallKnowledge
}
