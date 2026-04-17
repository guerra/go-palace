package halls_test

import (
	"testing"

	"github.com/guerra/go-palace/pkg/halls"
)

func TestDetect(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		room     string
		addedBy  string
		metadata map[string]any
		want     string
	}{
		{
			name: "DefaultKnowledge",
			want: halls.HallKnowledge,
		},
		{
			name:     "ConvosMode",
			metadata: map[string]any{"ingest_mode": "convos"},
			want:     halls.HallConversations,
		},
		{
			name:     "LegacyHallDiary",
			metadata: map[string]any{"hall": "hall_diary"},
			want:     halls.HallDiary,
		},
		{
			name:     "MetadataBareDiary",
			metadata: map[string]any{"hall": "diary"},
			want:     halls.HallDiary,
		},
		{
			name: "RoomDiary",
			room: "diary",
			want: halls.HallDiary,
		},
		{
			name:    "AddedByDiaryPrefix",
			addedBy: "diary_agent",
			want:    halls.HallDiary,
		},
		{
			name: "RoomTasks",
			room: "tasks",
			want: halls.HallTasks,
		},
		{
			name: "RoomTodo",
			room: "todo",
			want: halls.HallTasks,
		},
		{
			name: "RoomScratch",
			room: "scratch",
			want: halls.HallScratch,
		},
		{
			name: "RoomWip",
			room: "wip",
			want: halls.HallScratch,
		},
		{
			name: "RoomJournal",
			room: "journal",
			want: halls.HallJournal,
		},
		{
			// Metadata beats room: ingest_mode=convos wins over room=knowledge.
			name:     "PriorityMetaOverRoom",
			room:     "knowledge",
			metadata: map[string]any{"ingest_mode": "convos"},
			want:     halls.HallConversations,
		},
		{
			// Valid metadata hall wins over room.
			name:     "PriorityMetaHallOverRoom",
			room:     "tasks",
			metadata: map[string]any{"hall": "conversations"},
			want:     halls.HallConversations,
		},
		{
			// Unknown metadata hall falls through to room rule.
			name:     "UnknownMetaHallFallsThrough",
			room:     "tasks",
			metadata: map[string]any{"hall": "garbage"},
			want:     halls.HallTasks,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := halls.Detect(tc.content, tc.room, tc.addedBy, tc.metadata)
			if got != tc.want {
				t.Errorf("Detect(%q, %q, %q, %v) = %q; want %q",
					tc.content, tc.room, tc.addedBy, tc.metadata, got, tc.want)
			}
		})
	}
}

func TestIsValid_AcceptsAllConstants(t *testing.T) {
	for _, h := range halls.All {
		if !halls.IsValid(h) {
			t.Errorf("IsValid(%q) = false; want true", h)
		}
	}
}

func TestIsValid_RejectsUnknown(t *testing.T) {
	for _, bad := range []string{"", "hall_diary", "invalid", "Conversations"} {
		if halls.IsValid(bad) {
			t.Errorf("IsValid(%q) = true; want false", bad)
		}
	}
}

func TestAll_Exhaustiveness(t *testing.T) {
	if got := len(halls.All); got != 6 {
		t.Fatalf("len(All) = %d; want 6", got)
	}
	seen := make(map[string]bool)
	for _, h := range halls.All {
		if seen[h] {
			t.Errorf("All contains duplicate: %q", h)
		}
		seen[h] = true
	}
}

func TestHallArchivedNotValid(t *testing.T) {
	if halls.HallArchived != "archived" {
		t.Errorf("HallArchived = %q; want %q", halls.HallArchived, "archived")
	}
	if halls.IsValid(halls.HallArchived) {
		t.Errorf("IsValid(%q) = true; want false (archived is a transition state, not a canonical hall)",
			halls.HallArchived)
	}
	// Archived must be excluded from All.
	for _, h := range halls.All {
		if h == halls.HallArchived {
			t.Errorf("All contains HallArchived; archive is a transition state and must stay out of All")
		}
	}
}
