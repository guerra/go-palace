package palace

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// schemaStatements returns the ordered list of DDL statements that migrate a
// fresh database into the palace schema. It is safe to re-run on an
// existing database because every statement uses IF NOT EXISTS.
//
// NOTE: vec0 virtual table columns are declared using:
//   - `id text primary key` for the primary key
//   - `col text partition key` for filterable scalar partitions
//   - `embedding float[N]` for the vector column (N = dim parameter)
//
// This matches the constructor scenarios in sqlite-vec v0.1.6
// (sqlite-vec.c scenarios 1-4).
//
// v0.2: adds hall column on drawers (4th-tier taxonomy) and hall partition
// key on drawers_vec. For LEGACY databases where drawers already exists, the
// IF NOT EXISTS guard means these statements are no-ops — migrateToV2 in
// migration.go performs the ALTER + vec rebuild.
func schemaStatements(dim int) []string {
	return []string{
		`CREATE TABLE IF NOT EXISTS drawers (
        id TEXT PRIMARY KEY,
        document TEXT NOT NULL,
        wing TEXT NOT NULL,
        hall TEXT NOT NULL DEFAULT '',
        room TEXT NOT NULL,
        source_file TEXT NOT NULL,
        chunk_index INTEGER NOT NULL,
        added_by TEXT NOT NULL,
        filed_at TEXT NOT NULL,
        source_mtime REAL,
        metadata_json TEXT NOT NULL DEFAULT '{}'
    )`,
		`CREATE INDEX IF NOT EXISTS idx_drawers_source_file ON drawers(source_file)`,
		`CREATE INDEX IF NOT EXISTS idx_drawers_wing_room ON drawers(wing, room)`,
		fmt.Sprintf(`CREATE VIRTUAL TABLE IF NOT EXISTS drawers_vec USING vec0(
        id text primary key,
        wing text partition key,
        hall text partition key,
        room text partition key,
        source_file text partition key,
        embedding float[%d]
    )`, dim),
		`CREATE TABLE IF NOT EXISTS palace_meta (
        key TEXT PRIMARY KEY,
        value TEXT NOT NULL
    )`,
	}
}

// schemaVersionCurrent is the schema_version written by this binary on every
// successful Open() against either a fresh or freshly-migrated palace.
const schemaVersionCurrent = 2

// readStoredDim reads the embedding dimension from the palace_meta table.
// Returns (dim, true, nil) if found, (0, false, nil) if the table or key
// does not exist, or (0, false, err) on unexpected DB errors.
func readStoredDim(db *sql.DB) (int, bool, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM palace_meta WHERE key = 'embedding_dim'`).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		// "no such table" means legacy palace without palace_meta.
		if strings.Contains(err.Error(), "no such table") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("palace: read dim: %w", err)
	}
	dim, err := strconv.Atoi(val)
	if err != nil {
		return 0, false, fmt.Errorf("palace: bad stored dim %q: %w", val, err)
	}
	return dim, true, nil
}

// writeStoredDim writes the embedding dimension to the palace_meta table.
func writeStoredDim(db *sql.DB, dim int) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO palace_meta (key, value) VALUES ('embedding_dim', ?)`,
		strconv.Itoa(dim),
	)
	return err
}

// readSchemaVersion reads schema_version from palace_meta. Mirrors readStoredDim.
func readSchemaVersion(db *sql.DB) (int, bool, error) {
	var val string
	err := db.QueryRow(`SELECT value FROM palace_meta WHERE key = 'schema_version'`).Scan(&val)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, nil
		}
		if strings.Contains(err.Error(), "no such table") {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("palace: read schema_version: %w", err)
	}
	v, err := strconv.Atoi(val)
	if err != nil {
		return 0, false, fmt.Errorf("palace: bad schema_version %q: %w", val, err)
	}
	return v, true, nil
}

// writeSchemaVersion writes schema_version to palace_meta.
func writeSchemaVersion(db *sql.DB, v int) error {
	_, err := db.Exec(
		`INSERT OR REPLACE INTO palace_meta (key, value) VALUES ('schema_version', ?)`,
		strconv.Itoa(v),
	)
	return err
}
