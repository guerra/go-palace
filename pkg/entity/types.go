// Package entity extracts typed entities (people, places, projects, tools,
// dates, URLs, emails) from content strings and persists them to a pluggable
// store. Pure Go; no network, no ML.
//
// Sibling package internal/entity scans FILES (not strings) and surfaces only
// person/project/uncertain candidates plus an optional Wikipedia enrichment.
// pkg/entity is the content-scan counterpart used by in-memory pipelines (the
// forthcoming pkg/extractor, pkg/miner); internal/entity remains the CLI
// bootstrap scanner wired into `cmd/mempalace init`. The duplication is
// deliberate for gp-3 — unification is tracked as a future ticket.
//
// Invariant: every Entity surfaces with a BYTE offset into the source string
// such that content[e.Offset:e.Offset+len(e.Name)] == e.Name. Callers that
// need rune offsets must convert.
package entity

import (
	"errors"
	"sync"
	"time"
)

// EntityType names the category of a detected entity. Values are lowercase
// single words; persisted to sqlite as-is.
type EntityType string

// Canonical entity types surfaced by Detect.
const (
	TypePerson    EntityType = "person"
	TypePlace     EntityType = "place"
	TypeProject   EntityType = "project"
	TypeTool      EntityType = "tool"
	TypeDate      EntityType = "date"
	TypeURL       EntityType = "url"
	TypeEmail     EntityType = "email"
	TypeUncertain EntityType = "uncertain"
)

// AllTypes enumerates every valid EntityType in canonical order.
var AllTypes = []EntityType{
	TypePerson,
	TypePlace,
	TypeProject,
	TypeTool,
	TypeDate,
	TypeURL,
	TypeEmail,
	TypeUncertain,
}

// Entity is one detected occurrence. Offset is a BYTE offset into the source
// content string — content[Offset:Offset+len(Name)] == Name.
type Entity struct {
	Name       string     `json:"name"`
	Type       EntityType `json:"type"`
	Canonical  string     `json:"canonical"`
	Aliases    []string   `json:"aliases,omitempty"`
	Confidence float64    `json:"confidence"`
	Offset     int        `json:"offset"`
}

// EntityRow is the persisted shape of a Registry entry. AliasesJSON is a
// serialized []string so the store can treat it as an opaque text column.
// Mirrors palace.EntityRow field-for-field so PalaceStore can round-trip via
// a shallow copy.
type EntityRow struct {
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Canonical       string    `json:"canonical"`
	AliasesJSON     string    `json:"aliases_json"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	OccurrenceCount int       `json:"occurrence_count"`
}

// registryStore is the contract every Registry backend implements. Unexported
// so only MemoryStore and PalaceStore (shipped in this package) may satisfy
// it — this keeps the set of implementations finite and lets us evolve the
// interface without breaking third-party stores.
type registryStore interface {
	Load() (map[string]EntityRow, error)
	Upsert(row EntityRow) error
	Delete(name string) error
}

// RegistryOptions configures NewRegistry. Zero-value is valid: Store falls
// back to an in-memory MemoryStore and Seeds defaults to true (seeds must be
// explicitly disabled).
type RegistryOptions struct {
	// Store is the persistence backend. nil → an in-process MemoryStore.
	Store registryStore
	// SeedsDisabled, when true, skips pre-population of commonTools etc.
	// Prefer the inverted sense ("disabled") so the zero-value RegistryOptions{}
	// loads seeds, matching the Python oracle default.
	SeedsDisabled bool
}

// Registry is the in-memory view over a registryStore. Concurrent-safe.
type Registry struct {
	mu    sync.RWMutex
	store registryStore
	// data maps lowercase Name to the Entity view surfaced via Lookup/List.
	data map[string]Entity
	// rows mirrors data but carries the persistence metadata (FirstSeen,
	// LastSeen, OccurrenceCount) that Entity does not expose. Merge consults
	// rows to preserve FirstSeen and increment OccurrenceCount across
	// repeat observations of the same Name.
	rows map[string]EntityRow
}

// Sentinel errors.
var (
	// ErrEntityExists is returned by Add when an entity with the same
	// lowercase Name is already registered. Use Merge for upsert semantics.
	ErrEntityExists = errors.New("entity: already exists")
	// ErrEntityNotFound is returned when a Lookup / Delete target is absent.
	ErrEntityNotFound = errors.New("entity: not found")
)
