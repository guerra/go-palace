package palace

import (
	"database/sql"
	"fmt"
)

// schemaStatements is the ordered list of DDL statements that migrate a
// fresh database into the palace schema. It is safe to re-run on an
// existing database because every statement uses IF NOT EXISTS.
//
// NOTE: vec0 virtual table columns are declared using:
//   - `id text primary key` for the primary key
//   - `col text partition key` for filterable scalar partitions
//   - `embedding float[384]` for the vector column
//
// This matches the constructor scenarios in sqlite-vec v0.1.6
// (sqlite-vec.c scenarios 1-4).
var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS drawers (
        id TEXT PRIMARY KEY,
        document TEXT NOT NULL,
        wing TEXT NOT NULL,
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
	`CREATE VIRTUAL TABLE IF NOT EXISTS drawers_vec USING vec0(
        id text primary key,
        wing text partition key,
        room text partition key,
        source_file text partition key,
        embedding float[384]
    )`,
}

// migrate applies all DDL statements. It does NOT wrap in a transaction
// because sqlite-vec virtual table creation cannot be nested inside a
// user transaction — CREATE VIRTUAL TABLE allocates a shadow table and
// must commit outside any BEGIN.
func migrate(db *sql.DB) error {
	for i, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("palace: migrate stmt %d: %w", i, err)
		}
	}
	return nil
}
