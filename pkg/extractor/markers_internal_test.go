package extractor

import "testing"

// TestMarkerCount_PerSlice pins the compiled marker count per type. The
// numbers below reflect the shipped Go port; the Python oracle counts
// (DECISION=21, PREFERENCE=16, MILESTONE=33, PROBLEM=18, EMOTION=31) differ
// for milestone (+1 here) and emotion (-2 here) because of concat /
// deduplication choices during the port — see CHANGELOG gp-4. What this
// test guards is REGRESSION: any future drop or silent add without a paired
// constant update fails the build.
//
// Lives in the internal test file (same package) rather than extractor_test
// because the marker slices are package-private and must stay that way —
// callers should not depend on the concrete pattern set.
func TestMarkerCount_PerSlice(t *testing.T) {
	cases := []struct {
		name string
		got  int
		want int
	}{
		{"decision", len(decisionMarkers), 21},
		{"preference", len(preferenceMarkers), 16},
		{"milestone", len(milestoneMarkers), 34},
		{"problem", len(problemMarkers), 18},
		{"emotion", len(emotionMarkers), 29},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("%s markers: got %d, want %d (drop/add detected)", c.name, c.got, c.want)
		}
	}
}

// TestAllMarkersMapKeys ensures the allMarkers map covers every ClassificationType
// in AllTypes. A new type added to AllTypes without a matching entry here means
// the scoring loop silently skips that type.
func TestAllMarkersMapKeys(t *testing.T) {
	if len(allMarkers) != len(AllTypes) {
		t.Errorf("allMarkers has %d entries, AllTypes has %d", len(allMarkers), len(AllTypes))
	}
	for _, tp := range AllTypes {
		ms, ok := allMarkers[tp]
		if !ok {
			t.Errorf("allMarkers missing entry for type %q", tp)
			continue
		}
		if len(ms) == 0 {
			t.Errorf("allMarkers[%q] empty — no patterns compiled", tp)
		}
	}
}
