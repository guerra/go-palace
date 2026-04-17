package palace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"

	"github.com/guerra/go-palace/pkg/halls"
)

// classificationOverFetchMultiplier controls how many extra drawers sqlite-vec
// returns when QueryOptions.Classification is set. `classifications` lives in
// drawers.metadata_json (not on drawers_vec as a partition key), so the filter
// cannot be pushed into the KNN search — it runs as a post-filter on the
// vec-returned rows. To keep the caller's NResults roughly honored when the
// top-k candidates are not classification-dense, we over-fetch, post-filter,
// then slice to NResults. Bound by classificationOverFetchMax to keep latency
// predictable on huge palaces. This does NOT guarantee NResults — a palace
// with few classified drawers can still under-serve the caller — but it closes
// the "top-5 happen to be other classifications, return 0" foot-gun.
const (
	classificationOverFetchMultiplier = 20
	classificationOverFetchMax        = 1000
)

// Query runs a semantic search. It embeds text once then asks sqlite-vec for
// the top N matches, joining metadata from the drawers table. Similarity is
// computed as 1 - distance to match mempalace/searcher.py:75.
//
// When QueryOptions.Classification is non-empty, Query over-fetches from
// sqlite-vec (up to 20×NResults, capped at 1000) because the classification
// filter runs as a post-filter on drawers.metadata_json rather than as a vec
// partition key. Fewer than NResults may still return if the palace holds
// fewer classified drawers than requested.
func (p *Palace) Query(text string, opts QueryOptions) ([]SearchResult, error) {
	n := opts.NResults
	if n <= 0 {
		n = 5
	}
	// k is the vec-table KNN bound. For a classification filter, over-fetch
	// so the post-filter has enough candidates to usually honor NResults.
	k := n
	if opts.Classification != "" {
		k = n * classificationOverFetchMultiplier
		if k > classificationOverFetchMax {
			k = classificationOverFetchMax
		}
	}
	vecs, err := p.embedder.Embed([]string{text})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrEmbedder, err)
	}
	if len(vecs) != 1 {
		return nil, fmt.Errorf("%w: expected 1 vector, got %d", ErrEmbedder, len(vecs))
	}
	if len(vecs[0]) != p.dim {
		return nil, fmt.Errorf("%w: query vec has dim %d, schema requires %d",
			ErrEmbedder, len(vecs[0]), p.dim)
	}
	blob, err := vec.SerializeFloat32(vecs[0])
	if err != nil {
		return nil, fmt.Errorf("palace: serialize query vector: %w", err)
	}

	// Archived drawers are EXCLUDED from semantic Query by default. Compact
	// sets drawers.hall='archived' but leaves drawers_vec.hall on the
	// original partition (no per-row re-embed), so filtering on v.hall alone
	// would let archived rows leak into unfiltered searches — we filter on
	// d.hall instead. Callers who need to read archived rows use Get, which
	// bypasses the vec table entirely.
	query := `SELECT d.id, d.document, d.wing, d.hall, d.room, d.source_file,
                     d.chunk_index, d.added_by, d.filed_at, d.source_mtime,
                     d.metadata_json, v.distance
              FROM drawers_vec v
              INNER JOIN drawers d ON d.id = v.id
              WHERE v.embedding MATCH ? AND k = ?
                AND d.hall != ?`
	args := []any{blob, k, halls.HallArchived}
	if opts.Wing != "" {
		query += " AND v.wing = ?"
		args = append(args, opts.Wing)
	}
	if opts.Hall != "" {
		query += " AND v.hall = ?"
		args = append(args, opts.Hall)
	}
	if opts.Room != "" {
		query += " AND v.room = ?"
		args = append(args, opts.Room)
	}
	if opts.Classification != "" {
		query += ` AND EXISTS (
            SELECT 1 FROM json_each(d.metadata_json, '$.classifications') je
            WHERE json_extract(je.value, '$.type') = ?
        )`
		args = append(args, string(opts.Classification))
	}
	query += " ORDER BY v.distance"

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("palace: query: %w", err)
	}

	var out []SearchResult
	for rows.Next() {
		res, err := scanSearchResult(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return nil, fmt.Errorf("palace: query rows: %w", err)
	}
	// Close rows explicitly so a later TouchLastAccessed write tx does not
	// race the reader for the same connection.
	if err := rows.Close(); err != nil {
		return nil, fmt.Errorf("palace: query close: %w", err)
	}
	// Trim post-filter overflow back to the caller's NResults.
	if opts.Classification != "" && len(out) > n {
		out = out[:n]
	}
	// Opt-in last_accessed tracking. Runs AFTER rows.Close (defer above) so
	// the write tx does not contend with an open read cursor. Failures are
	// advisory — a bump miss never fails a search.
	if p.opts.TrackLastAccessed && len(out) > 0 {
		ids := make([]string, len(out))
		for i, r := range out {
			ids[i] = r.Drawer.ID
		}
		if err := p.TouchLastAccessed(ids); err != nil {
			slog.Warn("palace: touch last_accessed failed", "err", err)
		}
	}
	return out, nil
}

func scanSearchResult(rows *sql.Rows) (SearchResult, error) {
	var (
		d        Drawer
		filedAt  string
		metaJSON string
		mtime    sql.NullFloat64
		distance float64
	)
	if err := rows.Scan(
		&d.ID, &d.Document, &d.Wing, &d.Hall, &d.Room, &d.SourceFile,
		&d.ChunkIndex, &d.AddedBy, &filedAt, &mtime,
		&metaJSON, &distance,
	); err != nil {
		return SearchResult{}, fmt.Errorf("palace: query scan: %w", err)
	}
	if mtime.Valid {
		d.SourceMTime = mtime.Float64
	}
	if filedAt != "" {
		t, err := time.Parse(time.RFC3339Nano, filedAt)
		if err != nil {
			slog.Warn("palace: filed_at parse failed",
				"id", d.ID, "value", filedAt, "error", err)
		} else {
			d.FiledAt = t
		}
	}
	if metaJSON != "" {
		if err := json.Unmarshal([]byte(metaJSON), &d.Metadata); err != nil {
			slog.Warn("palace: metadata_json parse failed",
				"id", d.ID, "error", err)
		}
	}
	return SearchResult{Drawer: d, Similarity: 1 - distance}, nil
}
