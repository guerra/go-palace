package palace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// Query runs a semantic search. It embeds text once then asks sqlite-vec for
// the top N matches, joining metadata from the drawers table. Similarity is
// computed as 1 - distance to match mempalace/searcher.py:75.
func (p *Palace) Query(text string, opts QueryOptions) ([]SearchResult, error) {
	n := opts.NResults
	if n <= 0 {
		n = 5
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

	query := `SELECT d.id, d.document, d.wing, d.room, d.source_file,
                     d.chunk_index, d.added_by, d.filed_at, d.source_mtime,
                     d.metadata_json, v.distance
              FROM drawers_vec v
              INNER JOIN drawers d ON d.id = v.id
              WHERE v.embedding MATCH ? AND k = ?`
	args := []any{blob, n}
	if opts.Wing != "" {
		query += " AND v.wing = ?"
		args = append(args, opts.Wing)
	}
	if opts.Room != "" {
		query += " AND v.room = ?"
		args = append(args, opts.Room)
	}
	query += " ORDER BY v.distance"

	rows, err := p.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("palace: query: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []SearchResult
	for rows.Next() {
		res, err := scanSearchResult(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, res)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("palace: query rows: %w", err)
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
		&d.ID, &d.Document, &d.Wing, &d.Room, &d.SourceFile,
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
