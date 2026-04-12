package palace

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
)

// upsertOne inserts or replaces one drawer row plus its vec0 shadow row
// within an open transaction.
func upsertOne(tx *sql.Tx, d Drawer, vector []float32) error {
	var metaBytes []byte
	if d.Metadata == nil {
		// NOT NULL DEFAULT '{}' on metadata_json — preserve the invariant.
		metaBytes = []byte("{}")
	} else {
		b, err := json.Marshal(d.Metadata)
		if err != nil {
			return fmt.Errorf("palace: marshal metadata: %w", err)
		}
		metaBytes = b
	}

	filedAt := d.FiledAt
	if filedAt.IsZero() {
		filedAt = time.Now()
	}

	if _, err := tx.Exec(
		`INSERT INTO drawers (
            id, document, wing, room, source_file, chunk_index,
            added_by, filed_at, source_mtime, metadata_json
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
        ON CONFLICT(id) DO UPDATE SET
            document      = excluded.document,
            wing          = excluded.wing,
            room          = excluded.room,
            source_file   = excluded.source_file,
            chunk_index   = excluded.chunk_index,
            added_by      = excluded.added_by,
            filed_at      = excluded.filed_at,
            source_mtime  = excluded.source_mtime,
            metadata_json = excluded.metadata_json`,
		d.ID, d.Document, d.Wing, d.Room, d.SourceFile, d.ChunkIndex,
		d.AddedBy, filedAt.Format(time.RFC3339Nano), d.SourceMTime, string(metaBytes),
	); err != nil {
		return fmt.Errorf("palace: upsert drawers row: %w", err)
	}

	// vec0 does not support ON CONFLICT. Delete-then-insert gives upsert
	// semantics for the vector shadow.
	if _, err := tx.Exec(`DELETE FROM drawers_vec WHERE id = ?`, d.ID); err != nil {
		return fmt.Errorf("palace: delete vec row: %w", err)
	}

	blob, err := vec.SerializeFloat32(vector)
	if err != nil {
		return fmt.Errorf("palace: serialize vector: %w", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO drawers_vec (id, wing, room, source_file, embedding)
         VALUES (?, ?, ?, ?, ?)`,
		d.ID, d.Wing, d.Room, d.SourceFile, blob,
	); err != nil {
		return fmt.Errorf("palace: insert vec row: %w", err)
	}
	return nil
}

// scanDrawer reads one row from a SELECT that projects the drawers columns
// in the canonical order used by Get. Malformed filed_at / metadata_json
// values are logged at slog.Warn so upstream corruption is surfaced rather
// than silently discarded.
func scanDrawer(rows *sql.Rows) (Drawer, error) {
	var (
		d        Drawer
		filedAt  string
		metaJSON string
		mtime    sql.NullFloat64
	)
	if err := rows.Scan(
		&d.ID, &d.Document, &d.Wing, &d.Room, &d.SourceFile,
		&d.ChunkIndex, &d.AddedBy, &filedAt, &mtime, &metaJSON,
	); err != nil {
		return d, fmt.Errorf("palace: scan: %w", err)
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
	return d, nil
}
