package palace

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// TouchLastAccessed bumps each id's `metadata_json.$.last_accessed` to
// time.Now().UTC() in RFC3339Nano. All updates run in a single transaction.
// Missing ids are silently skipped (no error). A nil / empty slice is a
// no-op.
//
// json_set on a NOT NULL DEFAULT '{}' metadata_json column is safe — every
// row is guaranteed to hold parseable JSON per the schema invariant.
func (p *Palace) TouchLastAccessed(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	ts := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("palace: touch last_accessed begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	stmt, err := tx.Prepare(
		`UPDATE drawers SET metadata_json = json_set(metadata_json, '$.last_accessed', ?) WHERE id = ?`)
	if err != nil {
		return fmt.Errorf("palace: touch last_accessed prepare: %w", err)
	}
	defer func() { _ = stmt.Close() }()
	for _, id := range ids {
		if _, err := stmt.Exec(ts, id); err != nil {
			return fmt.Errorf("palace: touch last_accessed exec %s: %w", id, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("palace: touch last_accessed commit: %w", err)
	}
	return nil
}

// ColdDrawerIDs returns drawer ids considered "cold": whose
// `metadata_json.$.last_accessed` is older than before, OR which have no
// last_accessed at all AND whose filed_at is older than before (fallback
// for rows that pre-date TrackLastAccessed).
//
// protectedHalls is an allowlist of halls to EXCLUDE from the result —
// callers typically pass HallDiary/HallKnowledge to guard against deleting
// longform memory. An empty slice applies no exclusion.
//
// limit <= 0 returns all matches; otherwise caps to the first `limit` ids
// ordered by filed_at ascending so compact naturally targets the oldest.
func (p *Palace) ColdDrawerIDs(before time.Time, limit int, protectedHalls []string) ([]string, error) {
	beforeStr := before.UTC().Format(time.RFC3339Nano)
	// COALESCE pattern: if last_accessed is missing, fall back to filed_at.
	// Both are compared against the cutoff as lexicographically-ordered
	// RFC3339Nano strings, which is safe because the format is
	// zero-padded and collates identically to chronological order.
	query := `SELECT id FROM drawers
	          WHERE COALESCE(json_extract(metadata_json, '$.last_accessed'), filed_at) < ?`
	args := []any{beforeStr}
	if len(protectedHalls) > 0 {
		placeholders := make([]string, len(protectedHalls))
		for i, h := range protectedHalls {
			placeholders[i] = "?"
			args = append(args, h)
		}
		query += " AND hall NOT IN (" + strings.Join(placeholders, ",") + ")"
	}
	query += " ORDER BY filed_at ASC"
	if limit > 0 {
		query += " LIMIT ?"
		args = append(args, limit)
	}

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("palace: cold drawer ids query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("palace: cold drawer ids scan: %w", err)
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: cold drawer ids rows: %w", err)
	}
	return out, nil
}

// ArchiveDrawers marks drawers as archived by setting drawers.hall =
// halls.HallArchived. The drawers_vec partition hall is NOT rewritten to
// avoid a per-row re-embed; search.go compensates by filtering archived
// rows out on d.hall (not v.hall) in every Query, so archived drawers
// stop surfacing in semantic search regardless of the caller's Hall
// filter. Callers that need to READ archived rows use Get, which bypasses
// the vec table entirely. A future job may re-embed archived rows to
// repartition the vec table.
//
// All updates run in a single transaction. Missing ids are silently
// skipped. Returns the count of rows actually updated.
func (p *Palace) ArchiveDrawers(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("palace: archive begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids)+1)
	args = append(args, "archived")
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	res, err := tx.Exec(
		`UPDATE drawers SET hall = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return 0, fmt.Errorf("palace: archive update: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("palace: archive commit: %w", err)
	}
	return int(n), nil
}

// DeleteBatch removes many drawers atomically across drawers + drawers_vec
// in a single transaction. Mirrors ArchiveDrawers' shape so both lifecycle
// paths (archive / delete) share an O(batch) fsync profile. Missing ids
// are silently skipped. Returns the count of rows actually deleted from
// the drawers table (the vec-table delete may remove more or fewer rows
// if the two stores drifted — run pkg/repair to detect orphans).
func (p *Palace) DeleteBatch(ids []string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("palace: delete batch begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	in := strings.Join(placeholders, ",")

	res, err := tx.Exec(`DELETE FROM drawers WHERE id IN (`+in+`)`, args...)
	if err != nil {
		return 0, fmt.Errorf("palace: delete batch drawers: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM drawers_vec WHERE id IN (`+in+`)`, args...); err != nil {
		return 0, fmt.Errorf("palace: delete batch vec: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("palace: delete batch commit: %w", err)
	}
	return int(n), nil
}

// IntegrityCheck runs `PRAGMA integrity_check` and returns every row that
// is not the literal "ok". A nil/empty result means the database is clean.
// The scan is O(database size) and can take minutes on multi-GB palaces —
// pkg/repair exposes a SkipIntegrityCheck option for callers who need a
// fast-path.
func (p *Palace) IntegrityCheck() ([]string, error) {
	return runPragmaCheck(p.db, "PRAGMA integrity_check")
}

// QuickCheck runs `PRAGMA quick_check`, a faster variant of integrity_check
// that skips the UNIQUE/NOT NULL constraint pass. Suitable for liveness
// probes on large palaces.
func (p *Palace) QuickCheck() ([]string, error) {
	return runPragmaCheck(p.db, "PRAGMA quick_check")
}

func runPragmaCheck(db *sql.DB, stmt string) ([]string, error) {
	rows, err := db.Query(stmt)
	if err != nil {
		return nil, fmt.Errorf("palace: %s: %w", stmt, err)
	}
	defer func() { _ = rows.Close() }()

	var issues []string
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("palace: %s scan: %w", stmt, err)
		}
		if v != "ok" {
			issues = append(issues, v)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: %s rows: %w", stmt, err)
	}
	return issues, nil
}

// ScanOrphans reports ids present in only one of the two stores:
//
//   - drawerOrphanIDs: rows in drawers with no matching drawers_vec row.
//   - vecOrphanIDs:    rows in drawers_vec with no matching drawers row.
//
// The scan runs inside a read transaction so the two SELECTs see a
// consistent snapshot even if writers are active.
func (p *Palace) ScanOrphans() ([]string, []string, error) {
	tx, err := p.db.Begin()
	if err != nil {
		return nil, nil, fmt.Errorf("palace: scan orphans begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	drawerIDs, err := scanIDSet(tx, `SELECT id FROM drawers`)
	if err != nil {
		return nil, nil, err
	}
	vecIDs, err := scanIDSet(tx, `SELECT id FROM drawers_vec`)
	if err != nil {
		return nil, nil, err
	}

	var drawerOrphans, vecOrphans []string
	for id := range drawerIDs {
		if !vecIDs[id] {
			drawerOrphans = append(drawerOrphans, id)
		}
	}
	for id := range vecIDs {
		if !drawerIDs[id] {
			vecOrphans = append(vecOrphans, id)
		}
	}
	// Rollback (read-only) — defer handles it.
	return drawerOrphans, vecOrphans, nil
}

func scanIDSet(tx *sql.Tx, query string) (map[string]bool, error) {
	rows, err := tx.Query(query)
	if err != nil {
		return nil, fmt.Errorf("palace: scan orphans query: %w", err)
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string]bool)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("palace: scan orphans row: %w", err)
		}
		out[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: scan orphans rows: %w", err)
	}
	return out, nil
}

// DeleteOrphans removes the supplied orphan ids from the correct tables:
// drawerOrphans → drawers, vecOrphans → drawers_vec. Re-runs the orphan
// scan inside the write tx to avoid WAL races where a concurrent writer
// inserts a matching row between ScanOrphans and DeleteOrphans. Returns
// the total count of rows deleted across both tables.
func (p *Palace) DeleteOrphans(drawerOrphans, vecOrphans []string) (int, error) {
	if len(drawerOrphans) == 0 && len(vecOrphans) == 0 {
		return 0, nil
	}
	tx, err := p.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("palace: delete orphans begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	drawerIDs, err := scanIDSet(tx, `SELECT id FROM drawers`)
	if err != nil {
		return 0, err
	}
	vecIDs, err := scanIDSet(tx, `SELECT id FROM drawers_vec`)
	if err != nil {
		return 0, err
	}

	var total int
	// drawerOrphans: drawer rows that currently have NO matching vec row.
	confirmed := make([]string, 0, len(drawerOrphans))
	for _, id := range drawerOrphans {
		if drawerIDs[id] && !vecIDs[id] {
			confirmed = append(confirmed, id)
		}
	}
	if len(confirmed) > 0 {
		placeholders := make([]string, len(confirmed))
		args := make([]any, len(confirmed))
		for i, id := range confirmed {
			placeholders[i] = "?"
			args[i] = id
		}
		res, err := tx.Exec(
			`DELETE FROM drawers WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return 0, fmt.Errorf("palace: delete drawer orphans: %w", err)
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}

	// vecOrphans: vec rows that currently have NO matching drawer row.
	confirmed = confirmed[:0]
	for _, id := range vecOrphans {
		if vecIDs[id] && !drawerIDs[id] {
			confirmed = append(confirmed, id)
		}
	}
	if len(confirmed) > 0 {
		placeholders := make([]string, len(confirmed))
		args := make([]any, len(confirmed))
		for i, id := range confirmed {
			placeholders[i] = "?"
			args[i] = id
		}
		res, err := tx.Exec(
			`DELETE FROM drawers_vec WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
		if err != nil {
			return 0, fmt.Errorf("palace: delete vec orphans: %w", err)
		}
		n, _ := res.RowsAffected()
		total += int(n)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("palace: delete orphans commit: %w", err)
	}
	return total, nil
}

// EmbeddingDim returns the embedding dimension stored in palace_meta. When
// the key is missing (legacy palace) it returns (0, nil) so callers can
// distinguish "uninitialized" from a hard DB error.
func (p *Palace) EmbeddingDim() (int, error) {
	dim, found, err := readStoredDim(p.db)
	if err != nil {
		return 0, err
	}
	if !found {
		return 0, nil
	}
	return dim, nil
}

// ProbeEmbedderDim returns the dimension reported by the embedder wired
// into this Palace. Useful for repair dim-mismatch checks without having
// to re-run Embed.
func (p *Palace) ProbeEmbedderDim() int {
	return p.embedder.Dimension()
}
