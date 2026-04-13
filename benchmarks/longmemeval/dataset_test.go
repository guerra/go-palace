package main

import "testing"

func TestIsAbstention(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"q_123", false},
		{"q_123_abs", true},
		{"abs", false},
		{"q_abs_other", false},
	}
	for _, tt := range tests {
		got := IsAbstention(LongMemEntry{QuestionID: tt.id})
		if got != tt.want {
			t.Errorf("IsAbstention(%q) = %v, want %v", tt.id, got, tt.want)
		}
	}
}

func TestJoinUserTurns(t *testing.T) {
	session := []Turn{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "how are you"},
	}
	got := JoinUserTurns(session)
	want := "hello\nhow are you"
	if got != want {
		t.Errorf("JoinUserTurns = %q, want %q", got, want)
	}

	// Empty session.
	if JoinUserTurns(nil) != "" {
		t.Error("JoinUserTurns(nil) should be empty")
	}

	// No user turns.
	assistantOnly := []Turn{{Role: "assistant", Content: "hi"}}
	if JoinUserTurns(assistantOnly) != "" {
		t.Error("JoinUserTurns with no user turns should be empty")
	}
}

func TestSessionIDFromCorpusID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"sess_123", "sess_123"},
		{"sess_123_turn_4", "sess_123"},
		{"sess_turn_data_turn_3", "sess_turn_data"},
		{"turn_0", "turn_0"},
	}
	for _, tt := range tests {
		got := SessionIDFromCorpusID(tt.input)
		if got != tt.want {
			t.Errorf("SessionIDFromCorpusID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
