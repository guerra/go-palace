// Package extractor provides a 5-type heuristic classifier ported from the
// Python `general_extractor.py` (522 LOC) reference. Types are `decision`,
// `preference`, `milestone`, `problem`, and `emotion`.
//
// The package is pure Go: no LLM, no network, no CGO. Marker regex sets,
// sentiment word sets, resolution patterns, code-line detection and segment
// splitting are all ported verbatim from the Python oracle; scoring uses the
// Python formula `min(1.0, (score + length_bonus) / 5.0)`.
//
// This package replaces the now-removed `internal/extractor` (see gp-4 in
// CHANGELOG). One behavioural rename came with the lift: the Python oracle
// uses the string `"emotional"` for the fifth type, while the Go public API
// unifies on `"emotion"` (the public constant `TypeEmotion`). The internal
// marker map and all disambiguation helpers use `TypeEmotion` throughout,
// so Go code never surfaces the string `"emotional"`.
//
// Entry points: Extract (default options), ExtractWith (caller-supplied
// options), ExtractSegments (every segment including sub-threshold ones),
// Classify (single segment). Consumers that want to filter on classification
// type should rely on the named type constants rather than string literals.
package extractor

// ClassificationType is the named string type for one of the five
// supported classification categories.
type ClassificationType string

// The five classification types. Values are lowercase singular words.
const (
	TypeDecision   ClassificationType = "decision"
	TypePreference ClassificationType = "preference"
	TypeMilestone  ClassificationType = "milestone"
	TypeProblem    ClassificationType = "problem"
	TypeEmotion    ClassificationType = "emotion"
)

// AllTypes is the canonical iteration and tie-break order used by the
// classifier. Callers can range over this slice to enumerate every
// classification type deterministically.
var AllTypes = []ClassificationType{
	TypeDecision,
	TypePreference,
	TypeMilestone,
	TypeProblem,
	TypeEmotion,
}

// MetadataKey is the `Drawer.Metadata` key under which `pkg/palace` stores
// `[]Classification` after an auto-classified Upsert. Exporting this avoids
// stringly-typed duplication between palace and extractor.
const MetadataKey = "classifications"

// defaultMinConfidence is the threshold applied when ExtractorOptions.MinConfidence
// is negative. Matches the Python oracle default (0.3).
const defaultMinConfidence = 0.3

// segmentMinLen is the minimum trimmed segment length that gets scored.
// Shorter segments are passed through as unclassified Segments.
const segmentMinLen = 20

// Classification is one scored classification over a single segment.
// Evidence is the longest matching keyword/phrase seen for the winning type.
// Confidence is the Python formula `min(1.0, (score + length_bonus) / 5.0)`.
// Index is the 0-based position of the segment inside the source content.
//
// Security note: Evidence is a verbatim substring of the caller-supplied
// document (lowercased). Downstream consumers rendering Evidence into HTML,
// markdown, or other injection-sensitive formats MUST escape it — the
// extractor does not sanitize caller input.
type Classification struct {
	Type       ClassificationType `json:"type"`
	Evidence   string             `json:"evidence"`
	Confidence float64            `json:"confidence"`
	Index      int                `json:"index"`
}

// Segment pairs a split-out segment of source content with its classification.
// A Classification with Type == "" means the segment did not score above the
// threshold (or was below the length floor, or had no marker hits at all).
type Segment struct {
	Content        string         `json:"content"`
	Classification Classification `json:"classification"`
}

// ExtractorOptions controls Extract/ExtractSegments/Classify. The zero value
// is usable and matches the Python oracle default: MinConfidence == 0 uses
// the default threshold (0.3). A POSITIVE value is used as the exact
// threshold. A NEGATIVE value disables the filter entirely (every
// classified segment is returned). This mirrors the codebase convention
// established by palace.QueryOptions.NResults and pkg/dedup.DedupOptions
// where the zero-value is the sensible default.
type ExtractorOptions struct {
	// MinConfidence filters out classifications below this value.
	// Zero uses the default (0.3); negative disables the filter.
	MinConfidence float64
}

// threshold resolves MinConfidence to its effective value.
//   - MinConfidence == 0 → defaultMinConfidence (0.3)
//   - MinConfidence <  0 → 0 (no filter)
//   - MinConfidence >  0 → that exact value
func (o ExtractorOptions) threshold() float64 {
	if o.MinConfidence == 0 {
		return defaultMinConfidence
	}
	if o.MinConfidence < 0 {
		return 0
	}
	return o.MinConfidence
}
