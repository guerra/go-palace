package extractor_test

import (
	"strings"
	"testing"

	"github.com/guerra/go-palace/pkg/extractor"
)

// BenchmarkExtract10KB runs Extract over a ~10KB mixed-content fixture. The
// goal is to catch regressions from regex recompilation in the hot path
// (patterns must stay compiled at init). Budget is < 20ms per op on CI.
func BenchmarkExtract10KB(b *testing.B) {
	content := strings.Repeat(
		"We decided to use sqlite-vec because it works well.\n\n"+
			"I'm proud of the team and grateful for the effort.\n\n"+
			"The bug keeps crashing on macOS with a segfault.\n\n"+
			"Finally deployed v2.0 today after many months.\n\n"+
			"I always prefer snake_case over camelCase.\n\n",
		40,
	)
	if len(content) < 9000 {
		b.Fatalf("fixture too short: %d bytes", len(content))
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractor.Extract(content)
	}
}
