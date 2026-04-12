package sanitizer

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// --- TestPassthrough ---

func TestPassthrough(t *testing.T) {
	t.Run("short_query_unchanged", func(t *testing.T) {
		r := SanitizeQuery("What is Rust error handling?")
		assertEqual(t, r.CleanQuery, "What is Rust error handling?")
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("empty_query", func(t *testing.T) {
		r := SanitizeQuery("")
		assertEqual(t, r.CleanQuery, "")
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("empty_string_like_none", func(t *testing.T) {
		r := SanitizeQuery("")
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("exactly_safe_length", func(t *testing.T) {
		q := strings.Repeat("a", SafeQueryLength)
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("one_over_safe_length_triggers_sanitization", func(t *testing.T) {
		q := strings.Repeat("a", SafeQueryLength+1)
		r := SanitizeQuery(q)
		assertEqual(t, r.OriginalLength, SafeQueryLength+1)
	})

	t.Run("whitespace_only", func(t *testing.T) {
		r := SanitizeQuery("   \t\n  ")
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("single_char", func(t *testing.T) {
		r := SanitizeQuery("x")
		assertEqual(t, r.CleanQuery, "x")
		assertEqual(t, r.WasSanitized, false)
	})

	t.Run("leading_trailing_whitespace_short", func(t *testing.T) {
		r := SanitizeQuery("  hello world  ")
		assertEqual(t, r.CleanQuery, "hello world")
		assertEqual(t, r.WasSanitized, false)
	})
}

// --- TestQuestionExtraction ---

func TestQuestionExtraction(t *testing.T) {
	systemPrompt := strings.Repeat("You are a helpful assistant. ", 50)

	t.Run("question_at_end_of_long_text", func(t *testing.T) {
		q := systemPrompt + "What is the best way to handle errors in Rust?"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "error", "Rust")
		assertEqual(t, r.Method, "question_extraction")
	})

	t.Run("japanese_question_mark", func(t *testing.T) {
		q := systemPrompt + "Rust\u306e\u30a8\u30e9\u30fc\u30cf\u30f3\u30c9\u30ea\u30f3\u30b0\u65b9\u6cd5\u306f\uff1f"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "Rust", "\u30a8\u30e9\u30fc")
		assertEqual(t, r.Method, "question_extraction")
	})

	t.Run("multiple_questions_takes_last", func(t *testing.T) {
		q := systemPrompt + "What is Python?\nHow does Rust handle errors?"
		r := SanitizeQuery(q)
		assertContainsAny(t, r.CleanQuery, "Rust", "error")
	})

	t.Run("question_in_system_prompt_ignored_when_real_question_exists", func(t *testing.T) {
		sp := strings.Repeat("Are you ready to help? ", 30) + "\n"
		real := "What databases does MemPalace support?"
		r := SanitizeQuery(sp + real)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "MemPalace", "database")
	})

	t.Run("question_with_trailing_quote", func(t *testing.T) {
		q := systemPrompt + "What is 'Rust'?"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertEqual(t, r.Method, "question_extraction")
	})

	t.Run("fullwidth_exclamation_not_question", func(t *testing.T) {
		// Fullwidth ! should not trigger question extraction
		q := systemPrompt + "\u3053\u308c\u306f\u7d20\u6674\u3089\u3057\u3044\uff01"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		// Should NOT be question_extraction since no question mark
		if r.Method == "question_extraction" {
			t.Errorf("expected non-question method, got question_extraction")
		}
	})
}

// --- TestTailSentence ---

func TestTailSentence(t *testing.T) {
	systemPrompt := strings.Repeat("You are a helpful assistant. ", 50)

	t.Run("command_style_query", func(t *testing.T) {
		q := systemPrompt + "Show me all Rust error handling patterns"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "Rust", "error")
		assertMethodIn(t, r.Method, "tail_sentence", "question_extraction")
	})

	t.Run("keyword_style_query", func(t *testing.T) {
		sp := strings.Repeat("System configuration loaded. ", 60)
		q := sp + "\nMemPalace ChromaDB integration setup"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "MemPalace", "ChromaDB")
	})

	t.Run("multiple_short_segments_picks_long_one", func(t *testing.T) {
		sp := strings.Repeat("Short. ", 100)
		q := sp + "\nA sufficiently long tail segment for testing"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		assertContainsAny(t, r.CleanQuery, "sufficiently", "tail")
	})
}

// --- TestTailTruncation ---

func TestTailTruncation(t *testing.T) {
	t.Run("single_long_line_no_sentences", func(t *testing.T) {
		filler := strings.Join(repeatStr("ab", 200), "\n")
		r := SanitizeQuery(filler)
		assertEqual(t, r.WasSanitized, true)
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("clean_query %d > MaxQueryLength %d", len(r.CleanQuery), MaxQueryLength)
		}
		assertEqual(t, r.Method, "tail_truncation")
	})

	t.Run("truncation_preserves_tail", func(t *testing.T) {
		filler := strings.Repeat("x", 1000) + "IMPORTANT_QUERY_CONTENT"
		r := SanitizeQuery(filler)
		if !strings.Contains(r.CleanQuery, "IMPORTANT_QUERY_CONTENT") {
			t.Errorf("tail content not preserved: %q", r.CleanQuery)
		}
	})

	t.Run("all_newlines_short_segments", func(t *testing.T) {
		filler := strings.Join(repeatStr("xy", 300), "\n")
		r := SanitizeQuery(filler)
		assertEqual(t, r.WasSanitized, true)
		assertEqual(t, r.Method, "tail_truncation")
	})
}

// --- TestLengthGuards ---

func TestLengthGuards(t *testing.T) {
	t.Run("output_never_exceeds_max", func(t *testing.T) {
		longQ := strings.Repeat("a", 1000) + "?"
		sp := strings.Repeat("Context. ", 100)
		r := SanitizeQuery(sp + longQ)
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("clean_query %d > MaxQueryLength %d", len(r.CleanQuery), MaxQueryLength)
		}
	})

	t.Run("extraction_too_short_falls_through", func(t *testing.T) {
		sp := strings.Repeat("You are helpful. ", 50)
		q := sp + "\nOK?"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("max_length_boundary", func(t *testing.T) {
		q := strings.Repeat("b", MaxQueryLength+50)
		r := SanitizeQuery(q)
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("clean_query %d > MaxQueryLength", len(r.CleanQuery))
		}
	})

	t.Run("question_longer_than_max_gets_truncated", func(t *testing.T) {
		sp := strings.Repeat("Prefix. ", 50)
		longQ := strings.Repeat("w", 600) + "?"
		r := SanitizeQuery(sp + longQ)
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("clean_query %d > MaxQueryLength", len(r.CleanQuery))
		}
	})
}

// --- TestMetadata ---

func TestMetadata(t *testing.T) {
	systemPrompt := strings.Repeat("You are a helpful assistant. ", 50)

	t.Run("original_length_preserved", func(t *testing.T) {
		q := systemPrompt + "What is Rust?"
		r := SanitizeQuery(q)
		assertEqual(t, r.OriginalLength, len(strings.TrimSpace(q)))
	})

	t.Run("clean_length_matches_clean_query", func(t *testing.T) {
		q := systemPrompt + "What is Rust?"
		r := SanitizeQuery(q)
		assertEqual(t, r.CleanLength, len(r.CleanQuery))
	})

	t.Run("sanitized_flag_true_when_changed", func(t *testing.T) {
		q := systemPrompt + "What is Rust?"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("sanitized_flag_false_when_unchanged", func(t *testing.T) {
		r := SanitizeQuery("Short query")
		assertEqual(t, r.WasSanitized, false)
	})

	t.Run("method_field_always_set", func(t *testing.T) {
		for _, q := range []string{"", "short", strings.Repeat("long. ", 200)} {
			r := SanitizeQuery(q)
			if r.Method == "" {
				t.Errorf("method is empty for query of len %d", len(q))
			}
		}
	})

	t.Run("clean_length_never_negative", func(t *testing.T) {
		r := SanitizeQuery("")
		if r.CleanLength < 0 {
			t.Errorf("clean_length negative: %d", r.CleanLength)
		}
	})
}

// --- TestRealWorld ---

func TestRealWorld(t *testing.T) {
	t.Run("mempalace_wakeup_prepended", func(t *testing.T) {
		wakeup := strings.Repeat(
			"MemPalace loaded. Wings: technical, emotions, identity. "+
				"Rooms: chromadb-setup, error-handling, project-planning. "+
				"Total drawers: 234. Knowledge graph: 89 entities, 156 triples. "+
				"AAAK dialect active. Protocol: verify before responding. ", 5)
		real := "How did we decide on the database architecture?"
		r := SanitizeQuery(wakeup + real)
		assertEqual(t, r.WasSanitized, true)
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("exceeds max: %d", len(r.CleanQuery))
		}
		if len(r.CleanQuery) < MinQueryLength {
			t.Errorf("too short: %d", len(r.CleanQuery))
		}
	})

	t.Run("memory_md_prepended", func(t *testing.T) {
		memoryMd := strings.Repeat(
			"# Project Memory\n"+
				"## Architecture Decisions\n"+
				"- Use ChromaDB for vector storage\n"+
				"- MCP protocol for tool integration\n"+
				"- AAAK compression for efficient storage\n", 10)
		real := "What were the performance benchmarks for the search system?"
		r := SanitizeQuery(memoryMd + "\n" + real)
		assertEqual(t, r.WasSanitized, true)
		assertMethodIn(t, r.Method, "question_extraction", "tail_sentence")
	})

	t.Run("2000_char_system_prompt_with_question", func(t *testing.T) {
		sp := strings.Repeat("You are an AI assistant with access to tools. ", 45)
		real := "What is the status of the MemPalace project?"
		q := sp + real
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		if r.OriginalLength <= 2000 {
			t.Errorf("expected >2000 chars, got %d", r.OriginalLength)
		}
		if r.CleanLength > MaxQueryLength {
			t.Errorf("exceeds max: %d", r.CleanLength)
		}
		assertEqual(t, r.Method, "question_extraction")
	})

	t.Run("unicode_mixed_content", func(t *testing.T) {
		sp := strings.Repeat("\u3053\u308c\u306f\u30c6\u30b9\u30c8\u3067\u3059\u3002", 50)
		real := "Go\u8a00\u8a9e\u306e\u30a8\u30e9\u30fc\u30cf\u30f3\u30c9\u30ea\u30f3\u30b0\u306f\uff1f"
		r := SanitizeQuery(sp + "\n" + real)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("mixed_newlines_cr_lf", func(t *testing.T) {
		sp := strings.Repeat("Line of text.\r\n", 40)
		real := "What is the answer to this question?"
		r := SanitizeQuery(sp + real)
		assertEqual(t, r.WasSanitized, true)
	})
}

// --- Additional edge-case subtests for 44+ total ---

func TestEdgeCases(t *testing.T) {
	t.Run("only_question_marks", func(t *testing.T) {
		q := strings.Repeat("? ", 200)
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("newline_only_long", func(t *testing.T) {
		q := strings.Repeat("\n", 300)
		r := SanitizeQuery(q)
		// all-whitespace → passthrough
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("exactly_max_length", func(t *testing.T) {
		q := strings.Repeat("c", MaxQueryLength)
		r := SanitizeQuery(q)
		// 500 > 200 so it goes through pipeline
		if len(r.CleanQuery) > MaxQueryLength {
			t.Errorf("exceeds max: %d", len(r.CleanQuery))
		}
	})

	t.Run("question_at_start_not_end", func(t *testing.T) {
		sp := "What is this? " + strings.Repeat("No question here. ", 50)
		r := SanitizeQuery(sp)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("tab_separated_segments", func(t *testing.T) {
		sp := strings.Repeat("Segment. ", 100)
		q := sp + "\tActual query about Rust patterns"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("consecutive_delimiters", func(t *testing.T) {
		sp := strings.Repeat("...", 100) + "!!!" + strings.Repeat("???", 10)
		q := sp + "\nWhat is the answer?"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
	})

	t.Run("cjk_truncation_valid_utf8", func(t *testing.T) {
		// 200+ kanji runes, each 3 bytes. Byte-based truncation would split a rune.
		sp := strings.Repeat("漢", 300)
		q := sp + "質問は何ですか？"
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, true)
		if !utf8.ValidString(r.CleanQuery) {
			t.Error("CleanQuery contains invalid UTF-8")
		}
	})

	t.Run("rune_length_matches_python_semantics", func(t *testing.T) {
		// 150 kanji = 150 runes < SafeQueryLength(200), should passthrough
		q := strings.Repeat("漢", 150)
		r := SanitizeQuery(q)
		assertEqual(t, r.WasSanitized, false)
		assertEqual(t, r.OriginalLength, 150)
		assertEqual(t, r.Method, "passthrough")
	})

	t.Run("emoji_truncation_safe", func(t *testing.T) {
		sp := strings.Repeat("🔥", 300)
		real := "What is this?"
		r := SanitizeQuery(sp + "\n" + real)
		assertEqual(t, r.WasSanitized, true)
		if !utf8.ValidString(r.CleanQuery) {
			t.Error("CleanQuery contains invalid UTF-8 after emoji truncation")
		}
	})
}

// --- helpers ---

func assertEqual[T comparable](t *testing.T, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertContainsAny(t *testing.T, s string, substrs ...string) {
	t.Helper()
	lo := strings.ToLower(s)
	for _, sub := range substrs {
		if strings.Contains(lo, strings.ToLower(sub)) {
			return
		}
	}
	t.Errorf("%q does not contain any of %v", s, substrs)
}

func assertMethodIn(t *testing.T, got string, allowed ...string) {
	t.Helper()
	for _, a := range allowed {
		if got == a {
			return
		}
	}
	t.Errorf("method %q not in %v", got, allowed)
}

func repeatStr(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}
