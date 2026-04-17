package entity

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// NewRegistry constructs a Registry. A nil opts.Store falls back to an
// in-process MemoryStore. Seeds are loaded before persisted rows so
// persisted customizations win on conflict. A corrupt persisted row is
// skipped with a warning rather than aborting construction — consistent
// with backfillHalls's tolerance for malformed metadata.
func NewRegistry(opts RegistryOptions) (*Registry, error) {
	store := opts.Store
	if store == nil {
		store = NewMemoryStore()
	}

	rows, err := store.Load()
	if err != nil {
		return nil, fmt.Errorf("entity: registry load: %w", err)
	}

	data := map[string]Entity{}
	state := map[string]EntityRow{}

	// Seed path: commonTools are added as TypeTool entities unless disabled.
	// Seeds are in-memory only; they are NOT flushed to the store (see Close).
	if !opts.SeedsDisabled {
		for tool := range commonTools {
			key := strings.ToLower(tool)
			data[key] = Entity{
				Name:       tool,
				Type:       TypeTool,
				Canonical:  tool,
				Confidence: 1.0,
			}
			// Deliberately: no row for seeds — they exist in `data` only.
			// Close/Save skips entries without a matching `rows` entry.
		}
	}

	// Persisted rows override seeds. A corrupt row is skipped with Warn.
	for key, row := range rows {
		e, err := rowToEntity(row)
		if err != nil {
			slog.Warn("entity: skipping corrupt row", "key", key, "err", err)
			continue
		}
		k := strings.ToLower(e.Name)
		data[k] = e
		state[k] = row
	}

	return &Registry{store: store, data: data, rows: state}, nil
}

// Add inserts a new Entity. Returns ErrEntityExists if an Entity with the
// same (lowercase) Name is already registered. Use Merge for upsert.
//
// Store-first ordering: the persistence layer is updated before the in-
// memory view so a store failure leaves the Registry visible state
// unchanged (crash-safe — disk is authoritative).
func (r *Registry) Add(e Entity) error {
	if e.Name == "" {
		return fmt.Errorf("%w: empty name", ErrEntityNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(e.Name)
	if _, exists := r.data[key]; exists {
		return ErrEntityExists
	}
	now := time.Now().UTC()
	row, err := buildRow(e, now, now, 1)
	if err != nil {
		return err
	}
	if err := r.store.Upsert(row); err != nil {
		return fmt.Errorf("entity: add persist: %w", err)
	}
	r.data[key] = e
	r.rows[key] = row
	return nil
}

// Merge upserts an Entity. When a prior row exists, FirstSeen is preserved
// and OccurrenceCount is incremented; LastSeen advances to now. When the
// entry is fresh (or only present as a seed), it is treated as a new Add
// semantically but without the ErrEntityExists guard.
//
// Store-first ordering applies.
func (r *Registry) Merge(e Entity) error {
	if e.Name == "" {
		return fmt.Errorf("%w: empty name", ErrEntityNotFound)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	key := strings.ToLower(e.Name)
	now := time.Now().UTC()
	firstSeen := now
	count := 1
	if prior, ok := r.rows[key]; ok {
		firstSeen = prior.FirstSeen
		count = prior.OccurrenceCount + 1
	}
	row, err := buildRow(e, firstSeen, now, count)
	if err != nil {
		return err
	}
	if err := r.store.Upsert(row); err != nil {
		return fmt.Errorf("entity: merge persist: %w", err)
	}
	r.data[key] = e
	r.rows[key] = row
	return nil
}

// Lookup returns the Entity whose Name (case-insensitive) matches the
// argument, or one whose Aliases (case-insensitive) contains it. The
// alias scan is O(N) — acceptable for small registries.
func (r *Registry) Lookup(name string) (Entity, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	lower := strings.ToLower(name)
	if e, ok := r.data[lower]; ok {
		return e, true
	}
	for _, e := range r.data {
		for _, a := range e.Aliases {
			if strings.EqualFold(a, name) {
				return e, true
			}
		}
	}
	return Entity{}, false
}

// List returns a copy of all Entities sorted by Name (case-insensitive).
func (r *Registry) List() []Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Entity, 0, len(r.data))
	for _, e := range r.data {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// ListByType returns every Entity matching t.
func (r *Registry) ListByType(t EntityType) []Entity {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var out []Entity
	for _, e := range r.data {
		if e.Type == t {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

// Save is a no-op on the write-through Registry. It exists so callers that
// adopted the prior API (or future stores with deferred writes) stay
// compatible — every Add/Merge already persists through store.Upsert.
func (r *Registry) Save() error {
	return nil
}

// Close releases no resources: Registry is write-through, so there is
// nothing buffered. Seeds are deliberately NOT flushed to the store — they
// live in-memory only, preserving the arch-impact invariant that the
// entities table holds user-data only. Close is idempotent.
//
// The caller owns the underlying Palace (if any) and is responsible for
// closing it AFTER this Registry.
func (r *Registry) Close() error {
	return nil
}

// buildRow assembles an EntityRow from an Entity plus its persistence
// metadata. Aliases are sorted so AliasesJSON is reproducible across writes.
func buildRow(e Entity, firstSeen, lastSeen time.Time, occurrenceCount int) (EntityRow, error) {
	aliases := make([]string, len(e.Aliases))
	copy(aliases, e.Aliases)
	sort.Strings(aliases)
	b, err := json.Marshal(aliases)
	if err != nil {
		return EntityRow{}, fmt.Errorf("entity: marshal aliases: %w", err)
	}
	return EntityRow{
		Name:            e.Name,
		Type:            string(e.Type),
		Canonical:       e.Canonical,
		AliasesJSON:     string(b),
		FirstSeen:       firstSeen,
		LastSeen:        lastSeen,
		OccurrenceCount: occurrenceCount,
	}, nil
}

// rowToEntity deserializes a persisted row into an Entity. Persistence
// metadata (FirstSeen, LastSeen, OccurrenceCount) is not reflected in the
// Entity view — callers that need it consult Palace.EntityList or
// Registry internals directly.
func rowToEntity(row EntityRow) (Entity, error) {
	var aliases []string
	if row.AliasesJSON != "" && row.AliasesJSON != "[]" {
		if err := json.Unmarshal([]byte(row.AliasesJSON), &aliases); err != nil {
			return Entity{}, fmt.Errorf("entity: unmarshal aliases: %w", err)
		}
	}
	return Entity{
		Name:      row.Name,
		Type:      EntityType(row.Type),
		Canonical: row.Canonical,
		Aliases:   aliases,
	}, nil
}
