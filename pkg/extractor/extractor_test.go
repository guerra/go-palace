package extractor_test

import (
	"strings"
	"sync"
	"testing"

	"github.com/guerra/go-palace/pkg/extractor"
)

func TestExtractDecision(t *testing.T) {
	text := "We decided to go with sqlite-vec because it keeps the single-binary promise and integrates cleanly."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeDecision {
		t.Errorf("type = %q, want decision", cs[0].Type)
	}
	if cs[0].Confidence < 0.3 {
		t.Errorf("confidence = %f, want >= 0.3", cs[0].Confidence)
	}
	ev := strings.ToLower(cs[0].Evidence)
	if !strings.Contains(ev, "because") && !strings.Contains(ev, "we decided") {
		t.Errorf("evidence = %q, expected 'because' or 'we decided'", cs[0].Evidence)
	}
}

func TestExtractPreference(t *testing.T) {
	text := "I always use tabs instead of spaces in my code. My style is snake_case whenever possible."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypePreference {
		t.Errorf("type = %q, want preference", cs[0].Type)
	}
}

func TestExtractMilestone(t *testing.T) {
	text := "Finally got it working! The key was the off-by-one fix. Shipped v2.0 successfully to production."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeMilestone {
		t.Errorf("type = %q, want milestone", cs[0].Type)
	}
}

func TestExtractProblem(t *testing.T) {
	text := "The bug keeps failing on CI runs. Not sure of the root cause yet, but the build is broken."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeProblem {
		t.Errorf("type = %q, want problem", cs[0].Type)
	}
}

func TestExtractEmotion(t *testing.T) {
	text := "I'm so proud of you for this. I love you dearly. I'm happy and grateful for everything you gave."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeEmotion {
		t.Errorf("type = %q, want emotion", cs[0].Type)
	}
}

func TestExtractMixedTypes(t *testing.T) {
	// Five paragraphs, one per type. Assert at least one classification per
	// type appears in the result.
	text := strings.Join([]string{
		"We decided to go with sqlite-vec because it keeps the single-binary promise nicely.",
		"I always use tabs instead of spaces. My style is snake_case everywhere for consistency.",
		"Finally got it working! The key was the off-by-one fix and we shipped v2.0 successfully.",
		"The bug keeps failing on CI. Not sure of the root cause yet, but the build is broken sadly.",
		"I'm so proud of you. I love you. I'm happy and grateful for everything you gave me today.",
	}, "\n\n")
	cs := extractor.Extract(text)
	if len(cs) < 5 {
		t.Fatalf("expected >=5 classifications, got %d: %+v", len(cs), cs)
	}
	seen := map[extractor.ClassificationType]bool{}
	for _, c := range cs {
		seen[c.Type] = true
	}
	for _, want := range extractor.AllTypes {
		if !seen[want] {
			t.Errorf("missing classification type: %q (saw %+v)", want, cs)
		}
	}
}

func TestExtractNegative(t *testing.T) {
	// Multi-paragraph plain prose with no markers — expect zero classifications.
	text := "The weather is nice today outside the house.\n\n" +
		"I had coffee this morning at eight o'clock.\n\n" +
		"Here is the log of events from yesterday afternoon."
	cs := extractor.Extract(text)
	if len(cs) != 0 {
		t.Errorf("expected 0 classifications, got %d: %+v", len(cs), cs)
	}
}

func TestExtractEmptyContent(t *testing.T) {
	for _, tc := range []struct {
		name, input string
	}{
		{"empty", ""},
		{"whitespace", "   \n\n   "},
		{"one-char", "a"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cs := extractor.Extract(tc.input)
			if cs == nil {
				t.Errorf("expected empty slice, got nil")
			}
			if len(cs) != 0 {
				t.Errorf("expected 0 classifications, got %d", len(cs))
			}
		})
	}
}

func TestExtractShortSegmentFloor(t *testing.T) {
	// Short segment (< 20 chars) must be filtered out; a long decision
	// segment on another paragraph should survive.
	text := "it works.\n\nWe decided to go with Postgres because of the reliable JSONB support overall."
	cs := extractor.Extract(text)
	if len(cs) != 1 {
		t.Fatalf("expected 1 classification, got %d: %+v", len(cs), cs)
	}
	if cs[0].Type != extractor.TypeDecision {
		t.Errorf("type = %q, want decision", cs[0].Type)
	}
}

func TestExtractCodeLineStripped(t *testing.T) {
	// Decision keywords INSIDE a triple-backtick fence must not score. The
	// prose outside the fence scores normally. Both lines live inside ONE
	// paragraph so the extractor has to run extractProse.
	text := "The weather is nice and there are no classifiable markers here at all in this part.\n" +
		"```\nwe decided to go with Postgres because of JSONB support everywhere across the board\n```\n" +
		"Still nothing classifiable in the trailing prose line on this run."
	cs := extractor.Extract(text)
	if len(cs) != 0 {
		t.Errorf("expected 0 classifications (markers were in code), got %d: %+v", len(cs), cs)
	}
}

func TestExtractSpeakerTurns(t *testing.T) {
	// >= 3 turn markers triggers splitByTurns. Each decision-flavoured turn
	// should classify independently.
	text := "Human: We decided to use Postgres because of JSONB support and the reliability.\n" +
		"Assistant: Makes sense, the reason is the reliability guarantees offered there.\n" +
		"Human: I'm going to configure the default stack to match that approach going forward.\n" +
		"Assistant: Great approach, that architecture strategy is solid for the next phase."
	cs := extractor.Extract(text)
	if len(cs) < 2 {
		t.Fatalf("expected >=2 classifications from turn-split input, got %d", len(cs))
	}
}

func TestExtractParagraphSplit(t *testing.T) {
	text := "We decided to go with Postgres because JSONB support is excellent and very robust.\n\n" +
		"We picked Go because the single-binary deployment story is straightforward overall."
	cs := extractor.Extract(text)
	if len(cs) != 2 {
		t.Fatalf("expected 2 classifications (2 paragraphs), got %d: %+v", len(cs), cs)
	}
}

func TestExtractTieBreakOrder(t *testing.T) {
	// Craft a segment that triggers exactly one decision marker AND one
	// preference marker. AllTypes-order tie-break must pick decision.
	text := "i prefer this approach for our strategy moving forward into next quarter's planning."
	cs := extractor.Extract(text)
	if len(cs) != 1 {
		t.Fatalf("expected 1 classification, got %d: %+v", len(cs), cs)
	}
	if cs[0].Type != extractor.TypeDecision {
		t.Errorf("type = %q, want decision (stable tie-break)", cs[0].Type)
	}
}

func TestExtractDisambiguationResolvedProblem(t *testing.T) {
	// Problem-classified text with a resolution marker disambiguates to milestone.
	text := "The crash keeps happening during startup. Fixed it by catching the nil pointer properly."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeMilestone {
		t.Errorf("type = %q, want milestone (resolved problem)", cs[0].Type)
	}
}

func TestExtractDisambiguationProblemPositive(t *testing.T) {
	text := "The bug was awful but we finally got it working. Beautiful solution overall and we shipped it."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if cs[0].Type != extractor.TypeMilestone {
		t.Errorf("type = %q, want milestone (problem + positive)", cs[0].Type)
	}
}

func TestExtractConfidenceThresholdHigh(t *testing.T) {
	// One weak marker ("default"), no length bonus. Score = 1, confidence
	// = 0.2. Default 0.3 threshold filters it; 0.1 threshold admits it.
	text := "something about a default setting in the application configuration options folder"
	high := extractor.ExtractWith(text, extractor.ExtractorOptions{MinConfidence: 0.3})
	if len(high) != 0 {
		t.Errorf("expected 0 classifications at threshold 0.3, got %d: %+v", len(high), high)
	}
	low := extractor.ExtractWith(text, extractor.ExtractorOptions{MinConfidence: 0.1})
	if len(low) != 1 {
		t.Errorf("expected 1 classification at threshold 0.1, got %d: %+v", len(low), low)
	}
}

func TestExtractEvidenceLongestMatch(t *testing.T) {
	// Text hits both "because" (7 chars) and "pros and cons" (13 chars).
	// Evidence field should be the longest: "pros and cons".
	text := "We analysed pros and cons carefully because it matters a great deal going forward."
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	if strings.ToLower(cs[0].Evidence) != "pros and cons" {
		t.Errorf("evidence = %q, want %q", cs[0].Evidence, "pros and cons")
	}
}

func TestExtractLengthBonusMedium(t *testing.T) {
	// 200 < len(para) <= 500 → +1 bonus. "We decided" + "because" = 2 hits.
	// Score = 2 + 1 bonus = 3 → 0.6 confidence.
	prefix := "We decided to use Go because of this. "
	padding := strings.Repeat("plain prose filler sentence here. ", 7)
	text := prefix + padding // ~260 chars
	if len(text) <= 200 || len(text) > 500 {
		t.Fatalf("test fixture out of bounds: len=%d", len(text))
	}
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	// Expected ~0.6 (2 marker hits + 1 medium bonus = 3/5).
	if cs[0].Confidence < 0.55 || cs[0].Confidence > 0.65 {
		t.Errorf("confidence = %f, want ~0.6 (medium length bonus)", cs[0].Confidence)
	}
}

func TestExtractLengthBonusLong(t *testing.T) {
	prefix := "We decided to use Go because of this. "
	padding := strings.Repeat("plain prose filler sentence here. ", 20)
	text := prefix + padding // ~720 chars
	if len(text) <= 500 {
		t.Fatalf("test fixture too short: len=%d", len(text))
	}
	cs := extractor.Extract(text)
	if len(cs) == 0 {
		t.Fatalf("expected >=1 classification, got 0")
	}
	// 2 markers hit ("we decided", "because") + long bonus 2 = 4 → 0.8 confidence
	if cs[0].Confidence < 0.75 {
		t.Errorf("confidence = %f, want >= 0.75 (long length bonus)", cs[0].Confidence)
	}
}

func TestExtractSegmentOrder(t *testing.T) {
	text := strings.Join([]string{
		"We decided to use Postgres because JSONB is excellent and reliable for us.",
		"I always use tabs instead of spaces. My style is snake_case for readability.",
		"Finally shipped v2.0 today! Breakthrough fix landed on the main branch.",
	}, "\n\n")
	cs := extractor.Extract(text)
	if len(cs) != 3 {
		t.Fatalf("expected 3 classifications, got %d", len(cs))
	}
	for i, c := range cs {
		if c.Index != i {
			t.Errorf("cs[%d].Index = %d, want %d", i, c.Index, i)
		}
	}
}

func TestExtractDeterministic(t *testing.T) {
	text := strings.Join([]string{
		"We decided to use Postgres because JSONB is excellent overall and very reliable.",
		"I always use tabs instead of spaces. My style is snake_case everywhere else.",
		"Finally shipped v2.0 today! Breakthrough moment on the main branch with Alice.",
	}, "\n\n")
	first := extractor.Extract(text)
	for i := 0; i < 10; i++ {
		again := extractor.Extract(text)
		if len(first) != len(again) {
			t.Fatalf("nondeterministic length: %d vs %d", len(first), len(again))
		}
		for j := range first {
			if first[j] != again[j] {
				t.Errorf("nondeterministic result at run %d, idx %d: %+v vs %+v",
					i, j, first[j], again[j])
			}
		}
	}
}

func TestExtractSegments_BelowThresholdKept(t *testing.T) {
	text := "short.\n\nWe decided to use Postgres because JSONB is excellent overall and reliable."
	// Zero-value ExtractorOptions{} means "default threshold (0.3)".
	segs := extractor.ExtractSegments(text, extractor.ExtractorOptions{})
	if len(segs) != 2 {
		t.Fatalf("expected 2 segments, got %d", len(segs))
	}
	// First segment is short → kept with Type == "".
	if segs[0].Classification.Type != "" {
		t.Errorf("segs[0] type = %q, want empty", segs[0].Classification.Type)
	}
	if segs[0].Content != "short." {
		t.Errorf("segs[0] content = %q, want %q", segs[0].Content, "short.")
	}
	if segs[1].Classification.Type != extractor.TypeDecision {
		t.Errorf("segs[1] type = %q, want decision", segs[1].Classification.Type)
	}
}

func TestClassify_Single(t *testing.T) {
	c, ok := extractor.Classify(
		"Let's use Postgres because of JSONB support for our new service.",
		extractor.ExtractorOptions{},
	)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if c.Type != extractor.TypeDecision {
		t.Errorf("type = %q, want decision", c.Type)
	}
}

func TestClassify_NoHit(t *testing.T) {
	c, ok := extractor.Classify(
		"The weather is nice today.",
		extractor.ExtractorOptions{},
	)
	if ok {
		t.Errorf("expected ok=false, got %+v", c)
	}
}

func TestMarkerCount_Decision(t *testing.T) {
	if got, want := len(extractor.AllTypes), 5; got != want {
		t.Errorf("AllTypes len = %d, want %d", got, want)
	}
}

func TestExtractOptionsDefault(t *testing.T) {
	// Default threshold = 0.3 (both zero-value and Extract). Weak signal
	// scores 0.2 → filtered out by default. Negative MinConfidence disables
	// the filter entirely — weak signals classify.
	text := "something about a default setting in the application configuration options folder today"
	// MinConfidence < 0 is "no filter".
	noFilter := extractor.ExtractWith(text, extractor.ExtractorOptions{MinConfidence: -1})
	if len(noFilter) == 0 {
		t.Errorf("expected >=1 classification at MinConfidence<0 (no filter), got 0")
	}
	// Zero-value uses default 0.3 — filters out weak signals.
	zero := extractor.ExtractWith(text, extractor.ExtractorOptions{})
	if len(zero) != 0 {
		t.Errorf("expected 0 classifications at zero-value (default 0.3), got %d", len(zero))
	}
	// Explicit default via Extract matches zero-value behaviour.
	def := extractor.Extract(text)
	if len(def) != 0 {
		t.Errorf("expected 0 classifications at default threshold (weak signal), got %d", len(def))
	}
}

func TestConcurrentExtract(t *testing.T) {
	text := strings.Join([]string{
		"We decided to use Postgres because JSONB is excellent overall and very reliable.",
		"I always use tabs instead of spaces. My style is snake_case everywhere else.",
		"Finally shipped v2.0 today! Breakthrough moment on the main branch with Alice.",
	}, "\n\n")
	want := extractor.Extract(text)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				got := extractor.Extract(text)
				if len(got) != len(want) {
					t.Errorf("concurrent len mismatch: got %d want %d", len(got), len(want))
					return
				}
				// Byte-identical check — catches a race that preserves
				// length but swaps or corrupts entries.
				for k := range got {
					if got[k] != want[k] {
						t.Errorf("concurrent entry mismatch at %d: %+v vs %+v",
							k, got[k], want[k])
						return
					}
				}
			}
		}()
	}
	wg.Wait()
}

func TestEmotionTypeRename(t *testing.T) {
	// Public API must never surface "emotional".
	for _, t2 := range extractor.AllTypes {
		if string(t2) == "emotional" {
			t.Errorf("found 'emotional' in AllTypes — must be 'emotion'")
		}
	}
	if string(extractor.TypeEmotion) != "emotion" {
		t.Errorf("TypeEmotion = %q, want %q", extractor.TypeEmotion, "emotion")
	}
}
