package kg

import (
	"strings"

	"github.com/guerra/go-palace/pkg/entity"
	"github.com/guerra/go-palace/pkg/palace"
)

// DefaultExtractConfidence is the confidence assigned to every triple emitted
// by AutoExtractTriples. Heuristic surface-form matching can't justify high
// confidence, but 0.6 places the output above "guess" (0.5) while below the
// 0.8 floor used by direct user-provided facts.
const DefaultExtractConfidence = 0.6

// maxGapBytes bounds how far apart a subject entity, verb phrase, and object
// entity may sit in the document while still being considered a valid triple.
// Chosen as ~3 English words — long enough to tolerate inline qualifiers
// ("works at the amazing Acme") but short enough to reject cross-sentence
// pairs ("Alice is happy. Bob works at Acme.").
const maxGapBytes = 30

// pronouns are subject candidates we refuse to emit, because a first-person
// triple like ("i", "uses", "docker") is almost always noise.
var pronouns = map[string]bool{
	"i": true, "you": true, "we": true, "they": true, "he": true, "she": true, "it": true,
	"me": true, "us": true, "them": true, "him": true, "her": true,
	"my": true, "your": true, "our": true, "their": true, "his": true,
}

// VerbPattern declares one surface-form family ("works at" / "is at" / "joined"
// / "employed by") that maps to a single stable predicate ("works_at"). The
// type lists are allowlists: a subject-entity whose Type is not in
// SubjectTypes (or object-entity not in ObjectTypes) won't anchor the pattern.
type VerbPattern struct {
	Predicate    string
	SurfaceForms []string
	SubjectTypes []string
	ObjectTypes  []string
}

// VerbPatterns enumerates the 6 canonical verbs gp-5 extracts. Exported so
// downstream callers can extend or replace the set without forking extract.go.
// Each entry's SurfaceForms are checked case-insensitively against lowercased
// document text.
var VerbPatterns = []VerbPattern{
	{
		Predicate:    "works_at",
		SurfaceForms: []string{"works at", "is at", "joined", "employed by"},
		SubjectTypes: []string{"person", "uncertain"},
		ObjectTypes:  []string{"project", "tool", "place", "uncertain"},
	},
	{
		Predicate:    "lives_in",
		SurfaceForms: []string{"lives in", "moved to", "based in", "resides in"},
		SubjectTypes: []string{"person", "uncertain"},
		ObjectTypes:  []string{"place", "uncertain"},
	},
	{
		Predicate:    "uses",
		SurfaceForms: []string{"uses", "installed", "adopted", "using"},
		SubjectTypes: []string{"person", "project", "uncertain"},
		ObjectTypes:  []string{"tool", "project", "uncertain"},
	},
	{
		Predicate:    "prefers",
		SurfaceForms: []string{"prefers", "likes", "chose", "picked"},
		SubjectTypes: []string{"person", "uncertain"},
		ObjectTypes:  []string{"tool", "project", "uncertain"},
	},
	{
		Predicate:    "started",
		SurfaceForms: []string{"started", "launched", "founded", "began"},
		SubjectTypes: []string{"person", "uncertain"},
		ObjectTypes:  []string{"project", "uncertain"},
	},
	{
		Predicate:    "finished",
		SurfaceForms: []string{"finished", "completed", "shipped", "delivered"},
		SubjectTypes: []string{"person", "project", "uncertain"},
		ObjectTypes:  []string{"project", "uncertain"},
	},
}

// AutoExtractTriples scans drawer text for subject-verb-object matches anchored
// on the supplied entities. Returns palace.TripleRow values (not kg.Triple) so
// palace can consume them without importing pkg/kg.
//
// Algorithm:
//
//  1. For each VerbPattern × each SurfaceForm, find every lowercase occurrence
//     in the document.
//  2. For each occurrence at byte-offset vo:
//     - Subject: the latest entity ending at or before vo, within maxGapBytes,
//     whose Type is in pattern.SubjectTypes, and whose lowercased Name is
//     not a pronoun.
//     - Object: the earliest entity starting at or after vo+len(surface),
//     within maxGapBytes, whose Type is in pattern.ObjectTypes.
//     - Emit (subject.Canonical, predicate, object.Canonical, d.FiledAt).
//  3. Dedupe by (Subject, Predicate, Object).
//
// ValidFrom is formatted as "2006-01-02" from drawer.FiledAt. SourceCloset
// is drawer.ID. Confidence is DefaultExtractConfidence.
func AutoExtractTriples(d palace.Drawer, ents []palace.EntityMatch) []palace.TripleRow {
	if d.Document == "" || len(ents) == 0 {
		return nil
	}
	lower := strings.ToLower(d.Document)

	var out []palace.TripleRow
	seen := make(map[string]bool)

	for _, vp := range VerbPatterns {
		subjAllowed := toSet(vp.SubjectTypes)
		objAllowed := toSet(vp.ObjectTypes)
		for _, surface := range vp.SurfaceForms {
			sf := strings.ToLower(surface)
			// FindAll lowercase occurrences by walking with IndexOf.
			from := 0
			for {
				rel := strings.Index(lower[from:], sf)
				if rel < 0 {
					break
				}
				vo := from + rel
				verbEnd := vo + len(sf)
				from = verbEnd

				// Require a word boundary immediately before the surface
				// form — otherwise "usesful" would trigger the "uses" match.
				if vo > 0 && isWordByte(lower[vo-1]) {
					continue
				}
				if verbEnd < len(lower) && isWordByte(lower[verbEnd]) {
					continue
				}

				subj, okSubj := findSubject(ents, vo, subjAllowed)
				if !okSubj {
					continue
				}
				if pronouns[strings.ToLower(subj.Name)] {
					continue
				}
				obj, okObj := findObject(ents, verbEnd, objAllowed)
				if !okObj {
					continue
				}

				sName := subj.Canonical
				if sName == "" {
					sName = subj.Name
				}
				oName := obj.Canonical
				if oName == "" {
					oName = obj.Name
				}
				key := sName + "\x00" + vp.Predicate + "\x00" + oName
				if seen[key] {
					continue
				}
				seen[key] = true

				out = append(out, palace.TripleRow{
					Subject:      sName,
					Predicate:    vp.Predicate,
					Object:       oName,
					ValidFrom:    d.FiledAt.UTC().Format("2006-01-02"),
					Confidence:   DefaultExtractConfidence,
					SourceCloset: d.ID,
				})
			}
		}
	}
	return out
}

// findSubject picks the latest entity whose span ends at-or-before verbOffset
// and is within maxGapBytes. Returns the match + ok=true, or zero + false.
func findSubject(ents []palace.EntityMatch, verbOffset int, allowed map[string]bool) (palace.EntityMatch, bool) {
	var (
		best  palace.EntityMatch
		found bool
	)
	for _, e := range ents {
		spanEnd := e.Offset + len(e.Name)
		if spanEnd > verbOffset {
			continue
		}
		gap := verbOffset - spanEnd
		if gap > maxGapBytes {
			continue
		}
		if !allowed[e.Type] {
			continue
		}
		// "Latest" = highest spanEnd (closest to verb).
		if !found || (e.Offset+len(e.Name)) > (best.Offset+len(best.Name)) {
			best = e
			found = true
		}
	}
	return best, found
}

// findObject picks the earliest entity whose span starts at-or-after verbEnd
// and is within maxGapBytes.
func findObject(ents []palace.EntityMatch, verbEnd int, allowed map[string]bool) (palace.EntityMatch, bool) {
	var (
		best  palace.EntityMatch
		found bool
	)
	for _, e := range ents {
		if e.Offset < verbEnd {
			continue
		}
		gap := e.Offset - verbEnd
		if gap > maxGapBytes {
			continue
		}
		if !allowed[e.Type] {
			continue
		}
		if !found || e.Offset < best.Offset {
			best = e
			found = true
		}
	}
	return best, found
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

// isWordByte reports whether b is an ASCII letter, digit, or underscore. Used
// only for word-boundary checks against lowercase document bytes; non-ASCII
// bytes (UTF-8 continuation) are treated as non-word, which is conservative.
func isWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z':
		return true
	case b >= '0' && b <= '9':
		return true
	case b == '_':
		return true
	}
	return false
}

// NewPalaceAdapter wraps *KG as a palace.TripleSink. palace.UpsertBatch calls
// the adapter's AddTriple after a successful commit when AutoExtractKG is on.
// The adapter converts palace.TripleRow (fields mirrored only) into kg.Triple
// and delegates. Errors bubble up to the caller (palace logs and continues).
func NewPalaceAdapter(k *KG) palace.TripleSink {
	return &palaceAdapter{k: k}
}

type palaceAdapter struct {
	k *KG
}

func (a *palaceAdapter) AddTriple(row palace.TripleRow) (string, error) {
	return a.k.AddTriple(Triple{
		Subject:      row.Subject,
		Predicate:    row.Predicate,
		Object:       row.Object,
		ValidFrom:    row.ValidFrom,
		Confidence:   row.Confidence,
		SourceCloset: row.SourceCloset,
	})
}

// NewStatelessEntityDetector returns an EntityDetector that calls
// pkg/entity.Detect on every content string with no shared state. The
// detector is goroutine-safe because entity.Detect is pure. Callers who want
// persistence should wrap their own *entity.Registry instead.
//
// This constructor lives in pkg/kg (not pkg/palace) because palace must not
// import pkg/entity — the adapter is a one-line convenience that lives
// alongside the auto-extract it pairs with.
func NewStatelessEntityDetector() palace.EntityDetector {
	return statelessDetector{}
}

type statelessDetector struct{}

func (statelessDetector) DetectEntities(content string) []palace.EntityMatch {
	ents := entity.Detect(content)
	out := make([]palace.EntityMatch, 0, len(ents))
	for _, e := range ents {
		out = append(out, palace.EntityMatch{
			Name:       e.Name,
			Type:       string(e.Type),
			Canonical:  e.Canonical,
			Confidence: e.Confidence,
			Offset:     e.Offset,
		})
	}
	return out
}
