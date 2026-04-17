package entity

import (
	"strings"
	"sync"
)

// MemoryStore is the default in-process backend for Registry. It holds
// rows in a plain map keyed by lowercase Name and does not persist across
// process restarts. Safe for concurrent use.
type MemoryStore struct {
	mu   sync.RWMutex
	data map[string]EntityRow
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: map[string]EntityRow{}}
}

// Load returns a copy of the store contents. Copy semantics mirror
// PalaceStore.Load (which reads fresh rows from sqlite) so Registry code
// can treat the two stores identically.
func (m *MemoryStore) Load() (map[string]EntityRow, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make(map[string]EntityRow, len(m.data))
	for k, v := range m.data {
		out[k] = v
	}
	return out, nil
}

// Upsert inserts or replaces a row, keyed by lowercase Name.
func (m *MemoryStore) Upsert(row EntityRow) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[strings.ToLower(row.Name)] = row
	return nil
}

// Delete removes a row. Returns nil even when the key is absent — idempotent
// semantics match the palace-backed store.
func (m *MemoryStore) Delete(name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, strings.ToLower(name))
	return nil
}
