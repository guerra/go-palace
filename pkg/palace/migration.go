package palace

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/halls"
)

// migrate applies DDL statements and, for legacy v0.1.0 palaces, performs the
// v0.2 migration (hall column + vec table rebuild with hall partition + backfill
// and re-embed of all rows). It does NOT wrap anything in a transaction:
// sqlite-vec CREATE VIRTUAL TABLE allocates shadow tables and must commit
// outside any user BEGIN.
//
// Ordering:
//  1. Probe pre-DDL state — does `drawers_vec` exist with OLD shape (no `hall`
//     partition)? This distinguishes "fresh palace" (tables don't exist yet)
//     from "legacy empty palace" (v0.1 tables exist with 0 rows). A legacy
//     empty palace MUST rebuild the vec table even though `COUNT(*)` is 0.
//  2. Run idempotent schemaStatements (fresh DB → vN shape; legacy → no-op).
//  3. If schema_version already >= current, return (fast path).
//  4. Fail-fast on dim mismatch BEFORE any re-embed work.
//  5. If the pre-DDL probe said "no legacy vec table", this is a fresh palace
//     OR a v2+ palace. Either way schemaStatements already produced the
//     current shape — stamp schema_version and return. The v2→v3 bump is
//     additive-only (entities table + index) so no special migration is
//     needed; the IF NOT EXISTS statements in schemaStatements(…) take
//     care of adding the table to existing v2 palaces on next Open.
//  6. Otherwise: legacy v1 palace (empty or populated) → migrateToV2 (backup +
//     ALTER + rebuild vec + re-embed). reembedAll is a no-op for zero rows.
//     After migrateToV2 completes, writeSchemaVersion stamps schemaVersionCurrent
//     (currently v3 — v1 palaces skip straight to v3 because schemaStatements
//     already added the entities table during step 2).
//  7. Write schema_version.
func migrate(db *sql.DB, dim int, palacePath string, embedder embed.Embedder) error {
	// Step 1: probe BEFORE DDL so we can tell fresh from legacy-empty.
	legacyVec, err := hasLegacyVecTable(db)
	if err != nil {
		return fmt.Errorf("palace: probe legacy vec: %w", err)
	}

	for i, stmt := range schemaStatements(dim) {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("palace: migrate stmt %d: %w", i, err)
		}
	}

	v, found, err := readSchemaVersion(db)
	if err != nil {
		return err
	}
	if found && v >= schemaVersionCurrent {
		return nil
	}

	// Fail-fast on dim mismatch BEFORE any expensive re-embed.
	storedDim, dimFound, err := readStoredDim(db)
	if err != nil {
		return err
	}
	if dimFound && storedDim != dim {
		return fmt.Errorf("%w: palace has dim %d but embedder has dim %d (re-mine required)",
			ErrDimensionMismatch, storedDim, dim)
	}

	if !legacyVec {
		// Fresh palace: schemaStatements produced the v0.2 shape directly.
		// No vec rebuild needed; just stamp the version.
		if err := writeSchemaVersion(db, schemaVersionCurrent); err != nil {
			return fmt.Errorf("palace: write schema_version: %w", err)
		}
		return nil
	}

	// Legacy palace (empty or populated). Perform destructive migration.
	// reembedAll handles the zero-row case as a no-op loop.
	if err := migrateToV2(db, dim, palacePath, embedder); err != nil {
		return fmt.Errorf("%w: %v", ErrSchemaOutdated, err)
	}
	if err := writeSchemaVersion(db, schemaVersionCurrent); err != nil {
		return fmt.Errorf("palace: write schema_version post-migration: %w", err)
	}
	return nil
}

// hasLegacyVecTable reports whether `drawers_vec` exists with the OLD v0.1.0
// partition schema (no `hall` partition key). It reads sqlite_master.sql and
// checks the table DDL text. Returns false if the table does not exist (fresh
// palace) or if it already has the `hall` partition (post-migration).
func hasLegacyVecTable(db *sql.DB) (bool, error) {
	var ddl string
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = 'drawers_vec'`,
	).Scan(&ddl)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	// DDL text of a v0.2 vec table contains "hall text partition key"; v0.1
	// does not. Case-insensitive check in case sqlite normalizes casing.
	return !strings.Contains(strings.ToLower(ddl), "hall text partition key"), nil
}

// migrateToV2 performs the v0.1.0 → v0.2.0 migration:
//  1. File-copy backup to <path>.pre-v0.2.bak (and .wal if present).
//  2. ALTER TABLE drawers ADD COLUMN hall (idempotent via PRAGMA guard).
//  3. Backfill hall values via halls.Detect over existing rows.
//  4. DROP + CREATE drawers_vec with new partition schema.
//  5. Re-embed all drawers in batches, re-populate drawers_vec.
//
// The embedder is used because sqlite-vec does not expose shadow-table internals;
// the simplest way to repopulate a vec0 with new partition keys is to re-insert
// rows with fresh embeddings. The content is unchanged, so embeddings are
// deterministic for deterministic embedders (FakeEmbedder is SHA-256 based).
func migrateToV2(db *sql.DB, dim int, palacePath string, embedder embed.Embedder) error {
	slog.Info("palace: migrating to v0.2 schema", "path", palacePath)

	// 1. Backup.
	backupPath, err := createBackup(palacePath)
	if err != nil {
		return fmt.Errorf("backup: %w", err)
	}
	slog.Info("palace: backup created", "backup", backupPath)

	// 2. Add hall column if missing.
	hasHall, err := columnExists(db, "drawers", "hall")
	if err != nil {
		return fmt.Errorf("probe hall column: %w", err)
	}
	if !hasHall {
		// Try NOT NULL DEFAULT '' first; fall back to nullable if SQLite rejects.
		if _, err := db.Exec(`ALTER TABLE drawers ADD COLUMN hall TEXT NOT NULL DEFAULT ''`); err != nil {
			slog.Warn("palace: NOT NULL ADD COLUMN failed, retrying nullable", "error", err)
			if _, err2 := db.Exec(`ALTER TABLE drawers ADD COLUMN hall TEXT DEFAULT ''`); err2 != nil {
				return fmt.Errorf("alter table add hall: %w", err2)
			}
			// Upgrade existing NULLs to '' immediately so downstream code sees non-null.
			if _, err2 := db.Exec(`UPDATE drawers SET hall = '' WHERE hall IS NULL`); err2 != nil {
				return fmt.Errorf("backfill null halls: %w", err2)
			}
		}
	}

	// 3. Backfill hall via Detect heuristic (reads metadata_json).
	if err := backfillHalls(db); err != nil {
		return fmt.Errorf("backfill halls: %w", err)
	}

	// 4. Rebuild drawers_vec with new partition schema.
	if _, err := db.Exec(`DROP TABLE IF EXISTS drawers_vec`); err != nil {
		return fmt.Errorf("drop drawers_vec: %w", err)
	}
	createVec := fmt.Sprintf(`CREATE VIRTUAL TABLE drawers_vec USING vec0(
        id text primary key,
        wing text partition key,
        hall text partition key,
        room text partition key,
        source_file text partition key,
        embedding float[%d]
    )`, dim)
	if _, err := db.Exec(createVec); err != nil {
		return fmt.Errorf("recreate drawers_vec: %w", err)
	}

	// 5. Re-embed all drawers.
	if err := reembedAll(db, embedder); err != nil {
		return fmt.Errorf("re-embed: %w", err)
	}

	slog.Info("palace: v0.2 migration complete", "path", palacePath)
	return nil
}

// pragmaTableAllowlist is the set of table names that columnExists may probe.
// SQLite PRAGMA does not accept bound parameters for identifiers, so we must
// interpolate the name — the allowlist prevents injection via caller-supplied
// strings (see .claude/rules/go-patterns.md#sql-safety).
var pragmaTableAllowlist = map[string]struct{}{
	"drawers":     {},
	"palace_meta": {},
}

// columnExists checks PRAGMA table_info for a column. Idempotency guard for
// ALTER TABLE (since SQLite has no IF NOT EXISTS for ADD COLUMN).
func columnExists(db *sql.DB, table, column string) (bool, error) {
	if _, ok := pragmaTableAllowlist[table]; !ok {
		return false, fmt.Errorf("palace: columnExists: table %q not in allowlist", table)
	}
	rows, err := db.Query(fmt.Sprintf(`PRAGMA table_info(%s)`, table))
	if err != nil {
		return false, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var (
			cid       int
			name      string
			colType   string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &colType, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// backfillHalls walks drawers in batches, computes halls.Detect per row, and
// UPDATEs the hall column when the current value is empty. Rows that already
// have a non-empty hall (idempotent re-runs) are skipped. Termination is via
// `WHERE hall = '' OR hall IS NULL LIMIT batch` — updated rows fall out of the
// filter, so each batch shrinks toward zero.
func backfillHalls(db *sql.DB) error {
	const batch = 500
	type row struct {
		id, document, room, addedBy, metaJSON string
	}
	for {
		rows, err := db.Query(
			`SELECT id, document, room, added_by, metadata_json
             FROM drawers
             WHERE hall = '' OR hall IS NULL
             ORDER BY id
             LIMIT ?`, batch,
		)
		if err != nil {
			return err
		}
		var items []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.document, &r.room, &r.addedBy, &r.metaJSON); err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, r)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		if len(items) == 0 {
			break
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		stmt, err := tx.Prepare(`UPDATE drawers SET hall = ? WHERE id = ?`)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		for _, it := range items {
			var md map[string]any
			if it.metaJSON != "" {
				_ = json.Unmarshal([]byte(it.metaJSON), &md)
			}
			hall := halls.Detect(it.document, it.room, it.addedBy, md)
			if _, err := stmt.Exec(hall, it.id); err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				return err
			}
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
		if len(items) < batch {
			break
		}
	}
	return nil
}

// reembedAll re-computes embeddings for every drawer and inserts into the
// (freshly-recreated) drawers_vec table.
func reembedAll(db *sql.DB, embedder embed.Embedder) error {
	const batch = 100
	lastID := ""
	for {
		rows, err := db.Query(
			`SELECT id, document, wing, hall, room, source_file
             FROM drawers
             WHERE id > ?
             ORDER BY id
             LIMIT ?`, lastID, batch,
		)
		if err != nil {
			return err
		}
		type row struct {
			id, document, wing, hall, room, sourceFile string
		}
		var items []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.document, &r.wing, &r.hall, &r.room, &r.sourceFile); err != nil {
				_ = rows.Close()
				return err
			}
			items = append(items, r)
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return err
		}
		_ = rows.Close()
		if len(items) == 0 {
			break
		}

		docs := make([]string, len(items))
		for i, it := range items {
			docs[i] = it.document
		}
		vecs, err := embedder.Embed(docs)
		if err != nil {
			return fmt.Errorf("%w: %v", ErrEmbedder, err)
		}
		if len(vecs) != len(items) {
			return fmt.Errorf("%w: embedder returned %d vecs for %d docs",
				ErrEmbedder, len(vecs), len(items))
		}

		tx, err := db.Begin()
		if err != nil {
			return err
		}
		stmt, err := tx.Prepare(
			`INSERT INTO drawers_vec (id, wing, hall, room, source_file, embedding)
             VALUES (?, ?, ?, ?, ?, ?)`,
		)
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		for i, it := range items {
			blob, err := vec.SerializeFloat32(vecs[i])
			if err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				return fmt.Errorf("serialize: %w", err)
			}
			if _, err := stmt.Exec(it.id, it.wing, it.hall, it.room, it.sourceFile, blob); err != nil {
				_ = stmt.Close()
				_ = tx.Rollback()
				return fmt.Errorf("insert vec row: %w", err)
			}
		}
		_ = stmt.Close()
		if err := tx.Commit(); err != nil {
			return err
		}
		lastID = items[len(items)-1].id
		if len(items) < batch {
			break
		}
	}
	return nil
}
