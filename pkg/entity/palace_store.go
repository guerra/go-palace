// palace_store.go is the ONLY file in pkg/entity that imports pkg/palace.
// All SQL stays in pkg/palace; PalaceStore is a thin adapter bridging
// pkg/entity.EntityRow ↔ pkg/palace.EntityRow (structurally parallel).
//
// Lifecycle rule: close the Registry BEFORE closing the underlying Palace.
// The Registry's Save flush requires a live db handle.

package entity

import (
	"strings"

	"github.com/guerra/go-palace/pkg/palace"
)

// PalaceStore persists Registry rows into the palace's `entities` table.
type PalaceStore struct {
	p *palace.Palace
}

// NewPalaceStore wraps p as a registryStore. The caller retains ownership
// of p — PalaceStore does NOT close p on its own.
func NewPalaceStore(p *palace.Palace) *PalaceStore {
	return &PalaceStore{p: p}
}

// Load returns every persisted entity row keyed by lowercase Name.
func (s *PalaceStore) Load() (map[string]EntityRow, error) {
	rows, err := s.p.EntityList()
	if err != nil {
		return nil, err
	}
	out := make(map[string]EntityRow, len(rows))
	for _, r := range rows {
		out[strings.ToLower(r.Name)] = palaceRowToEntityRow(r)
	}
	return out, nil
}

// Upsert writes a row to the palace.
func (s *PalaceStore) Upsert(row EntityRow) error {
	return s.p.EntityUpsert(entityRowToPalaceRow(row))
}

// Delete removes a row by name (case-insensitive). Idempotent.
func (s *PalaceStore) Delete(name string) error {
	return s.p.EntityDelete(name)
}

// palaceRowToEntityRow converts the palace-owned EntityRow to the
// pkg/entity EntityRow. The two structs are field-for-field parallel —
// the duplication exists so pkg/palace does not need to import pkg/entity.
func palaceRowToEntityRow(r palace.EntityRow) EntityRow {
	return EntityRow{
		Name:            r.Name,
		Type:            r.Type,
		Canonical:       r.Canonical,
		AliasesJSON:     r.AliasesJSON,
		FirstSeen:       r.FirstSeen,
		LastSeen:        r.LastSeen,
		OccurrenceCount: r.OccurrenceCount,
	}
}

// entityRowToPalaceRow converts the pkg/entity EntityRow to the palace-owned
// EntityRow.
func entityRowToPalaceRow(r EntityRow) palace.EntityRow {
	return palace.EntityRow{
		Name:            r.Name,
		Type:            r.Type,
		Canonical:       r.Canonical,
		AliasesJSON:     r.AliasesJSON,
		FirstSeen:       r.FirstSeen,
		LastSeen:        r.LastSeen,
		OccurrenceCount: r.OccurrenceCount,
	}
}
