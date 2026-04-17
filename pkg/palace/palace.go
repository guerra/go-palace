// Package palace is the ONLY package that imports mattn/go-sqlite3 and
// sqlite-vec-go-bindings. See .agents/arch/system.arch.md:82-85 — every
// other package must go through the palace.Palace API. Violations are
// coupling breaks caught by /ac:validation-code-review.
package palace

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/normalize"
)

// Sentinel errors returned by the palace API.
var (
	// ErrNoPalace indicates the palace database could not be opened.
	ErrNoPalace = errors.New("palace: not found")
	// ErrNotFound indicates a requested drawer was not present.
	ErrNotFound = errors.New("palace: drawer not found")
	// ErrEmbedder indicates the embedder rejected or failed on the input.
	ErrEmbedder = errors.New("palace: embedder error")
	// ErrUnknownWhereKey is returned by Get when the where map contains
	// a key that is not in the allowlist of filterable columns.
	ErrUnknownWhereKey = errors.New("palace: unknown where key")
)

// DefaultEmbeddingDim is the default vector dimension (all-MiniLM-L6-v2).
// Used by tests and as the assumed dimension for legacy palaces.
const DefaultEmbeddingDim = 384

// ErrDimensionMismatch indicates the embedder dimension does not match
// the dimension stored in an existing palace.
var ErrDimensionMismatch = errors.New("palace: dimension mismatch")

// ErrSchemaOutdated indicates the palace schema is older than this binary
// expects and the migration attempt failed.
var ErrSchemaOutdated = errors.New("palace: schema outdated; migration failed")

// ErrDedupCrossPartition is returned by MergeAndDelete when the winner and
// any loser drawer do not share the same (wing, hall, source_file)
// partition. Dedup MUST NOT cross partition boundaries — this sentinel
// surfaces misuse rather than silently corrupting partition semantics.
var ErrDedupCrossPartition = errors.New("palace: dedup partition mismatch")

// ErrEntityNotFound is returned by entity-table operations when the target
// row does not exist OR when the caller supplies an empty Name.
var ErrEntityNotFound = errors.New("palace: entity not found")

// allowedWhereKeys is the allowlist of columns Get may filter on.
// Used to prevent SQL injection via dynamic column names.
var allowedWhereKeys = map[string]string{
	"wing":        "wing",
	"hall":        "hall",
	"room":        "room",
	"source_file": "source_file",
	"added_by":    "added_by",
	"chunk_index": "chunk_index",
}

// Palace is a sqlite-vec-backed drawer store. Construct via Open.
type Palace struct {
	db       *sql.DB
	embedder embed.Embedder
	path     string
	dim      int
}

// Drawer is one stored memory cell. ID is deterministic via ComputeDrawerID.
// Metadata carries any extra key-value pairs that do not fit the typed fields.
// Hall is the 4th-tier taxonomy bucket (see pkg/halls). Empty string is
// legal (legacy rows), but new code should set it via halls.Detect.
type Drawer struct {
	ID          string
	Document    string
	Wing        string
	Hall        string
	Room        string
	SourceFile  string
	ChunkIndex  int
	AddedBy     string
	FiledAt     time.Time
	SourceMTime float64
	Metadata    map[string]any
}

// SearchResult is a drawer paired with a similarity score in [0,1].
type SearchResult struct {
	Drawer     Drawer
	Similarity float64
}

// GetOptions controls a Get call. Where is matched against an allowlist of
// columns; unknown keys return ErrUnknownWhereKey. Limit <= 0 means "all".
// Offset is only applied when Limit > 0 — SQLite requires LIMIT to accompany
// OFFSET, and rather than paper over that with `LIMIT -1`, the coupling is
// surfaced explicitly here so callers don't paginate unexpectedly.
type GetOptions struct {
	Where  map[string]string
	Limit  int
	Offset int
}

// QueryOptions controls a semantic Query call. Wing, Hall and Room restrict
// results; empty strings mean "no filter". NResults <= 0 defaults to 5.
type QueryOptions struct {
	Wing     string
	Hall     string
	Room     string
	NResults int
}

func init() { vec.Auto() }

// Open opens (or creates) the sqlite-vec database at path, applies the
// schema, and returns a ready-to-use Palace. The embedder is stored for
// later Upsert / Query calls. The embedder's dimension is validated
// against any previously stored dimension — a mismatch is rejected at
// Open so callers don't discover it via a cryptic sqlite-vec BLOB-length
// error on first insert.
func Open(path string, e embed.Embedder) (*Palace, error) {
	if e == nil {
		return nil, fmt.Errorf("%w: nil embedder", ErrEmbedder)
	}
	embDim := e.Dimension()
	if embDim <= 0 {
		return nil, fmt.Errorf("%w: embedder dim %d invalid", ErrEmbedder, embDim)
	}
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("%w: open %s: %v", ErrNoPalace, path, err)
	}
	// Restrict palace DB to owner-only (go-sqlite3 creates with umask defaults).
	if err := os.Chmod(path, 0o600); err != nil && !os.IsNotExist(err) {
		_ = db.Close()
		return nil, fmt.Errorf("palace: chmod %s: %w", path, err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("palace: enable WAL: %w", err)
	}
	if err := migrate(db, embDim, path, e); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Check stored dimension for existing palaces.
	storedDim, found, err := readStoredDim(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	if found && storedDim != embDim {
		_ = db.Close()
		return nil, fmt.Errorf("%w: palace has dim %d but embedder has dim %d (re-mine required)",
			ErrDimensionMismatch, storedDim, embDim)
	}
	if !found {
		// New palace or legacy palace without meta — store the dimension.
		if err := writeStoredDim(db, embDim); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("palace: write dim: %w", err)
		}
	}
	return &Palace{db: db, embedder: e, path: path, dim: embDim}, nil
}

// Close releases the underlying database. It is safe to call multiple times.
func (p *Palace) Close() error {
	if p == nil || p.db == nil {
		return nil
	}
	return p.db.Close()
}

// Path returns the database path Open was called with.
func (p *Palace) Path() string { return p.path }

// Upsert inserts or replaces one drawer.
func (p *Palace) Upsert(d Drawer) error {
	return p.UpsertBatch([]Drawer{d})
}

// UpsertBatch inserts or replaces many drawers atomically.
func (p *Palace) UpsertBatch(ds []Drawer) error {
	if len(ds) == 0 {
		return nil
	}
	// Embed all documents in one call for efficiency. Documents are
	// normalized before embedding so incidental whitespace / CRLF / Unicode
	// form differences do not fragment the vector space. The raw
	// caller-supplied string is what upsertOne stores — dual-state: stored
	// raw, embedded normalized. See pkg/normalize.
	docs := make([]string, len(ds))
	for i, d := range ds {
		docs[i] = normalize.Normalize(d.Document)
	}
	vecs, err := p.embedder.Embed(docs)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrEmbedder, err)
	}
	if len(vecs) != len(ds) {
		return fmt.Errorf("%w: embedder returned %d vecs for %d docs",
			ErrEmbedder, len(vecs), len(ds))
	}
	for i, v := range vecs {
		if len(v) != p.dim {
			return fmt.Errorf("%w: vec[%d] has dim %d, schema requires %d",
				ErrEmbedder, i, len(v), p.dim)
		}
	}

	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("palace: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for i, d := range ds {
		if err := upsertOne(tx, d, vecs[i]); err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("palace: commit: %w", err)
	}
	return nil
}

// Get retrieves drawers matching the filters in opts. Results are ordered by
// (wing, room, source_file, chunk_index) for deterministic output.
func (p *Palace) Get(opts GetOptions) ([]Drawer, error) {
	var (
		clauses []string
		args    []any
	)
	for k, v := range opts.Where {
		col, ok := allowedWhereKeys[k]
		if !ok {
			return nil, fmt.Errorf("%w: %q", ErrUnknownWhereKey, k)
		}
		clauses = append(clauses, col+" = ?")
		args = append(args, v)
	}

	query := `SELECT id, document, wing, hall, room, source_file, chunk_index,
                added_by, filed_at, source_mtime, metadata_json
              FROM drawers`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	query += " ORDER BY wing, room, source_file, chunk_index"
	if opts.Limit > 0 {
		query += " LIMIT ?"
		args = append(args, opts.Limit)
		if opts.Offset > 0 {
			query += " OFFSET ?"
			args = append(args, opts.Offset)
		}
	}

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("palace: get query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Drawer
	for rows.Next() {
		d, err := scanDrawer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: get rows: %w", err)
	}
	return out, nil
}

// GetByIDs fetches specific drawers by their IDs. Unknown IDs are silently
// skipped (no error). The returned slice preserves database order, not input
// order.
func (p *Palace) GetByIDs(ids []string) ([]Drawer, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	query := `SELECT id, document, wing, hall, room, source_file, chunk_index,
                added_by, filed_at, source_mtime, metadata_json
              FROM drawers WHERE id IN (` + strings.Join(placeholders, ",") + `)
              ORDER BY wing, room, source_file, chunk_index`
	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("palace: get_by_ids query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []Drawer
	for rows.Next() {
		d, err := scanDrawer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: get_by_ids rows: %w", err)
	}
	return out, nil
}

// Delete removes one drawer by id. Returns ErrNotFound if id did not exist.
func (p *Palace) Delete(id string) error {
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("palace: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(`DELETE FROM drawers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("palace: delete drawers: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(`DELETE FROM drawers_vec WHERE id = ?`, id); err != nil {
		return fmt.Errorf("palace: delete vec: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("palace: commit delete: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// MergeAndDelete atomically merges mergedMeta into the winner drawer's
// metadata_json and deletes the loserIDs. All loser IDs must share the
// same (wing, hall, source_file) partition as the winner; any mismatch
// rolls back and returns ErrDedupCrossPartition.
//
// The metadata merge is SHALLOW: keys in mergedMeta overwrite keys in the
// winner's existing metadata. Callers that need deep-merge semantics must
// compute the union themselves before calling.
//
// A nil / empty loserIDs is a no-op that still applies mergedMeta to the
// winner's metadata (pass nil mergedMeta for a pure no-op).
// winnerID == "" returns ErrNotFound; a winnerID that does not exist
// returns ErrNotFound. Missing loser rows are ignored (warn only).
func (p *Palace) MergeAndDelete(winnerID string, loserIDs []string, mergedMeta map[string]any) error {
	if winnerID == "" {
		return ErrNotFound
	}
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("palace: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var (
		wWing, wHall, wSrc, wMetaJSON string
	)
	row := tx.QueryRow(
		`SELECT wing, hall, source_file, metadata_json FROM drawers WHERE id = ?`,
		winnerID,
	)
	if err := row.Scan(&wWing, &wHall, &wSrc, &wMetaJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return fmt.Errorf("palace: merge fetch winner: %w", err)
	}

	// Partition guard: every loser must match winner's (wing, hall, source_file).
	// Batch the fetch into one IN (...) round-trip — small-group dedup is the
	// common case but large groups should not pay per-row query overhead.
	candidateIDs := make([]string, 0, len(loserIDs))
	seen := map[string]bool{winnerID: true} // dedupe input + skip winner
	for _, lid := range loserIDs {
		if lid == "" || seen[lid] {
			continue
		}
		seen[lid] = true
		candidateIDs = append(candidateIDs, lid)
	}

	validLosers := make([]string, 0, len(candidateIDs))
	if len(candidateIDs) > 0 {
		placeholders := make([]string, len(candidateIDs))
		args := make([]any, len(candidateIDs))
		for i, id := range candidateIDs {
			placeholders[i] = "?"
			args[i] = id
		}
		rows, err := tx.Query(
			`SELECT id, wing, hall, source_file FROM drawers WHERE id IN (`+
				strings.Join(placeholders, ",")+`)`, args...,
		)
		if err != nil {
			return fmt.Errorf("palace: merge fetch losers: %w", err)
		}
		loserInfo := make(map[string][3]string, len(candidateIDs))
		for rows.Next() {
			var lid, lWing, lHall, lSrc string
			if err := rows.Scan(&lid, &lWing, &lHall, &lSrc); err != nil {
				_ = rows.Close()
				return fmt.Errorf("palace: merge scan loser: %w", err)
			}
			loserInfo[lid] = [3]string{lWing, lHall, lSrc}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("palace: merge loser rows: %w", err)
		}
		_ = rows.Close()

		for _, lid := range candidateIDs {
			info, ok := loserInfo[lid]
			if !ok {
				// Missing loser — skip silently. Dedup may race with other
				// writers; an absent loser is simply already-gone.
				continue
			}
			if info[0] != wWing || info[1] != wHall || info[2] != wSrc {
				return fmt.Errorf("%w: winner=(%s,%s,%s) loser %s=(%s,%s,%s)",
					ErrDedupCrossPartition, wWing, wHall, wSrc, lid, info[0], info[1], info[2])
			}
			validLosers = append(validLosers, lid)
		}
	}

	// Shallow merge mergedMeta over winner metadata.
	if len(mergedMeta) > 0 {
		winnerMeta := map[string]any{}
		if wMetaJSON != "" {
			if err := json.Unmarshal([]byte(wMetaJSON), &winnerMeta); err != nil {
				return fmt.Errorf("palace: parse winner metadata: %w", err)
			}
		}
		for k, v := range mergedMeta {
			winnerMeta[k] = v
		}
		newMeta, err := json.Marshal(winnerMeta)
		if err != nil {
			return fmt.Errorf("palace: marshal merged metadata: %w", err)
		}
		if _, err := tx.Exec(
			`UPDATE drawers SET metadata_json = ? WHERE id = ?`,
			string(newMeta), winnerID,
		); err != nil {
			return fmt.Errorf("palace: update winner metadata: %w", err)
		}
	}

	// Batch delete losers from both tables.
	if len(validLosers) > 0 {
		placeholders := make([]string, len(validLosers))
		args := make([]any, len(validLosers))
		for i, id := range validLosers {
			placeholders[i] = "?"
			args[i] = id
		}
		in := strings.Join(placeholders, ",")
		if _, err := tx.Exec(
			`DELETE FROM drawers WHERE id IN (`+in+`)`, args...,
		); err != nil {
			return fmt.Errorf("palace: delete losers drawers: %w", err)
		}
		if _, err := tx.Exec(
			`DELETE FROM drawers_vec WHERE id IN (`+in+`)`, args...,
		); err != nil {
			return fmt.Errorf("palace: delete losers vec: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("palace: commit merge: %w", err)
	}
	return nil
}

// Count returns the total number of drawers. A DB error is returned
// explicitly so callers can distinguish "empty palace" (n=0, err=nil) from
// "broken palace" (n=0, err != nil).
func (p *Palace) Count() (int, error) {
	var n int
	if err := p.db.QueryRow(`SELECT COUNT(*) FROM drawers`).Scan(&n); err != nil {
		return 0, fmt.Errorf("palace: count: %w", err)
	}
	return n, nil
}

// CountWhere counts drawers matching the filters. Unknown keys return
// ErrUnknownWhereKey so callers can't silently mistype a filter.
func (p *Palace) CountWhere(where map[string]string) (int, error) {
	var (
		clauses []string
		args    []any
	)
	for k, v := range where {
		col, ok := allowedWhereKeys[k]
		if !ok {
			return 0, fmt.Errorf("%w: %q", ErrUnknownWhereKey, k)
		}
		clauses = append(clauses, col+" = ?")
		args = append(args, v)
	}

	query := `SELECT COUNT(*) FROM drawers`
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	var n int
	if err := p.db.QueryRow(query, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("palace: count_where: %w", err)
	}
	return n, nil
}

// ComputeDrawerID returns the deterministic drawer ID used by the Python
// oracle: "drawer_<wing>_<room>_<first 24 hex chars of sha256(sourceFile+chunkIndex)>".
// See mempalace/miner.py:377.
func ComputeDrawerID(wing, room, sourceFile string, chunkIndex int) string {
	sum := sha256.Sum256([]byte(sourceFile + strconv.Itoa(chunkIndex)))
	return fmt.Sprintf("drawer_%s_%s_%s", wing, room, hex.EncodeToString(sum[:])[:24])
}

// EntityRow is the persisted shape of a pkg/entity.Registry entry. Kept
// here (not in pkg/entity) so palace remains the single sqlite-aware
// package; pkg/entity.PalaceStore converts between the two struct shapes.
// AliasesJSON is a JSON-encoded []string; palace treats it as opaque text.
type EntityRow struct {
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Canonical       string    `json:"canonical"`
	AliasesJSON     string    `json:"aliases_json"`
	FirstSeen       time.Time `json:"first_seen"`
	LastSeen        time.Time `json:"last_seen"`
	OccurrenceCount int       `json:"occurrence_count"`
}

// EntityUpsert inserts or replaces a row in the entities table. The row's
// lowercase Name is used as the primary key so Lookups stay
// case-insensitive. An empty Name returns ErrEntityNotFound.
func (p *Palace) EntityUpsert(row EntityRow) error {
	if row.Name == "" {
		return fmt.Errorf("%w: empty name", ErrEntityNotFound)
	}
	id := strings.ToLower(row.Name)
	aliases := row.AliasesJSON
	if aliases == "" {
		aliases = "[]"
	}
	tx, err := p.db.Begin()
	if err != nil {
		return fmt.Errorf("palace: entity upsert begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	_, err = tx.Exec(
		`INSERT OR REPLACE INTO entities
         (id, name, type, canonical, aliases_json, first_seen, last_seen, occurrence_count)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, row.Name, row.Type, row.Canonical, aliases,
		row.FirstSeen.UTC().Format(time.RFC3339Nano),
		row.LastSeen.UTC().Format(time.RFC3339Nano),
		row.OccurrenceCount,
	)
	if err != nil {
		return fmt.Errorf("palace: entity upsert exec: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("palace: entity upsert commit: %w", err)
	}
	return nil
}

// EntityList returns every row in the entities table, ordered by name.
func (p *Palace) EntityList() ([]EntityRow, error) {
	rows, err := p.db.Query(
		`SELECT name, type, canonical, aliases_json, first_seen, last_seen, occurrence_count
         FROM entities ORDER BY name`,
	)
	if err != nil {
		return nil, fmt.Errorf("palace: entity list query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []EntityRow
	for rows.Next() {
		var (
			row                 EntityRow
			firstSeen, lastSeen string
		)
		if err := rows.Scan(
			&row.Name, &row.Type, &row.Canonical, &row.AliasesJSON,
			&firstSeen, &lastSeen, &row.OccurrenceCount,
		); err != nil {
			return nil, fmt.Errorf("palace: entity list scan: %w", err)
		}
		if firstSeen != "" {
			t, err := time.Parse(time.RFC3339Nano, firstSeen)
			if err != nil {
				return nil, fmt.Errorf("palace: entity list first_seen parse: %w", err)
			}
			row.FirstSeen = t
		}
		if lastSeen != "" {
			t, err := time.Parse(time.RFC3339Nano, lastSeen)
			if err != nil {
				return nil, fmt.Errorf("palace: entity list last_seen parse: %w", err)
			}
			row.LastSeen = t
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: entity list rows: %w", err)
	}
	return out, nil
}

// EntityDelete removes the row whose lowercase Name matches. Idempotent:
// deleting a missing row returns nil (mirrors MemoryStore semantics).
func (p *Palace) EntityDelete(name string) error {
	if name == "" {
		return fmt.Errorf("%w: empty name", ErrEntityNotFound)
	}
	id := strings.ToLower(name)
	if _, err := p.db.Exec(`DELETE FROM entities WHERE id = ?`, id); err != nil {
		return fmt.Errorf("palace: entity delete: %w", err)
	}
	return nil
}
