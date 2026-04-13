// Package palace is the ONLY package that imports mattn/go-sqlite3 and
// sqlite-vec-go-bindings. See .agents/arch/system.arch.md:82-85 — every
// other package must go through the palace.Palace API. Violations are
// coupling breaks caught by /ac:validation-code-review.
package palace

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
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

// allowedWhereKeys is the allowlist of columns Get may filter on.
// Used to prevent SQL injection via dynamic column names.
var allowedWhereKeys = map[string]string{
	"wing":        "wing",
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
type Drawer struct {
	ID          string
	Document    string
	Wing        string
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

// QueryOptions controls a semantic Query call. Wing and Room restrict
// results; empty strings mean "no filter". NResults <= 0 defaults to 5.
type QueryOptions struct {
	Wing     string
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
	if err := migrate(db, embDim); err != nil {
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
	// Embed all documents in one call for efficiency.
	docs := make([]string, len(ds))
	for i, d := range ds {
		docs[i] = d.Document
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

	query := `SELECT id, document, wing, room, source_file, chunk_index,
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
	query := `SELECT id, document, wing, room, source_file, chunk_index,
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
