// Package kg implements a temporal knowledge graph triple store backed by
// a SEPARATE SQLite database (not the palace sqlite-vec DB).
// Ports knowledge_graph.py.
package kg

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

// KG is a temporal knowledge graph backed by SQLite.
type KG struct {
	db   *sql.DB
	path string
}

// Triple represents a subject-predicate-object relationship.
type Triple struct {
	Subject      string
	Predicate    string
	Object       string
	ValidFrom    string
	ValidTo      string
	Confidence   float64
	SourceCloset string
	SourceFile   string
}

// Fact is a query result row from the knowledge graph.
type Fact struct {
	Direction    string
	Subject      string
	Predicate    string
	Object       string
	ValidFrom    string
	ValidTo      string
	Confidence   float64
	SourceCloset string
	Current      bool
}

// QueryOpts controls entity queries.
type QueryOpts struct {
	AsOf      string // date filter
	Direction string // "outgoing" (default), "incoming", "both"
}

// KGStats summarises the knowledge graph.
type KGStats struct {
	Entities          int
	Triples           int
	CurrentFacts      int
	ExpiredFacts      int
	RelationshipTypes []string
}

// Open opens or creates a knowledge graph database at dbPath.
func Open(dbPath string) (*KG, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o700); err != nil {
		return nil, fmt.Errorf("kg: mkdir: %w", err)
	}
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("kg: open: %w", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("kg: wal: %w", err)
	}
	if err := initSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &KG{db: db, path: dbPath}, nil
}

func initSchema(db *sql.DB) error {
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS entities (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			type TEXT DEFAULT 'unknown',
			properties TEXT DEFAULT '{}',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP
		);

		CREATE TABLE IF NOT EXISTS triples (
			id TEXT PRIMARY KEY,
			subject TEXT NOT NULL,
			predicate TEXT NOT NULL,
			object TEXT NOT NULL,
			valid_from TEXT,
			valid_to TEXT,
			confidence REAL DEFAULT 1.0,
			source_closet TEXT,
			source_file TEXT,
			extracted_at TEXT DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (subject) REFERENCES entities(id),
			FOREIGN KEY (object) REFERENCES entities(id)
		);

		CREATE INDEX IF NOT EXISTS idx_triples_subject ON triples(subject);
		CREATE INDEX IF NOT EXISTS idx_triples_object ON triples(object);
		CREATE INDEX IF NOT EXISTS idx_triples_predicate ON triples(predicate);
		CREATE INDEX IF NOT EXISTS idx_triples_valid ON triples(valid_from, valid_to);
	`)
	return err
}

// Close releases the database connection.
func (kg *KG) Close() error {
	if kg == nil || kg.db == nil {
		return nil
	}
	return kg.db.Close()
}

// EntityID normalises a name to an entity ID: lowercase, spaces to underscores, remove apostrophes.
// The transform is lossy: distinct names may collide (e.g. "O'Brien" and "OBrien" both map to
// "obrien"). Callers that create entities should be aware that collisions cause silent merges.
func EntityID(name string) string {
	return strings.NewReplacer(" ", "_", "'", "").Replace(strings.ToLower(name))
}

// AddEntity adds or replaces an entity.
func (kg *KG) AddEntity(name, entityType string, props map[string]any) (string, error) {
	eid := EntityID(name)
	propsJSON, _ := json.Marshal(props)
	if props == nil {
		propsJSON = []byte("{}")
	}
	_, err := kg.db.Exec(
		"INSERT OR REPLACE INTO entities (id, name, type, properties) VALUES (?, ?, ?, ?)",
		eid, name, entityType, string(propsJSON),
	)
	if err != nil {
		return "", fmt.Errorf("kg: add entity: %w", err)
	}
	return eid, nil
}

// AddTriple adds a relationship triple. Auto-creates entities. Deduplicates active triples.
func (kg *KG) AddTriple(t Triple) (string, error) {
	subID := EntityID(t.Subject)
	objID := EntityID(t.Object)
	pred := strings.ToLower(strings.ReplaceAll(t.Predicate, " ", "_"))

	tx, err := kg.db.Begin()
	if err != nil {
		return "", fmt.Errorf("kg: begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Auto-create entities
	if _, err := tx.Exec("INSERT OR IGNORE INTO entities (id, name) VALUES (?, ?)", subID, t.Subject); err != nil {
		return "", fmt.Errorf("kg: auto-create subject: %w", err)
	}
	if _, err := tx.Exec("INSERT OR IGNORE INTO entities (id, name) VALUES (?, ?)", objID, t.Object); err != nil {
		return "", fmt.Errorf("kg: auto-create object: %w", err)
	}

	// Check for existing active triple
	var existingID string
	err = tx.QueryRow(
		"SELECT id FROM triples WHERE subject=? AND predicate=? AND object=? AND valid_to IS NULL",
		subID, pred, objID,
	).Scan(&existingID)
	if err == nil {
		_ = tx.Commit()
		return existingID, nil
	}
	if err != sql.ErrNoRows {
		return "", fmt.Errorf("kg: check existing: %w", err)
	}

	// Generate triple ID with SHA-256 hash
	hashInput := fmt.Sprintf("%s%s", t.ValidFrom, time.Now().Format(time.RFC3339Nano))
	hash := sha256.Sum256([]byte(hashInput))
	tripleID := fmt.Sprintf("t_%s_%s_%s_%s", subID, pred, objID, hex.EncodeToString(hash[:])[:12])

	conf := t.Confidence
	if conf == 0 {
		conf = 1.0
	}

	_, err = tx.Exec(
		`INSERT INTO triples (id, subject, predicate, object, valid_from, valid_to, confidence, source_closet, source_file)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		tripleID, subID, pred, objID, nilStr(t.ValidFrom), nilStr(t.ValidTo), conf, nilStr(t.SourceCloset), nilStr(t.SourceFile),
	)
	if err != nil {
		return "", fmt.Errorf("kg: insert triple: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("kg: commit: %w", err)
	}
	return tripleID, nil
}

// Invalidate marks matching active triples as no longer valid.
func (kg *KG) Invalidate(subject, predicate, object, ended string) error {
	subID := EntityID(subject)
	objID := EntityID(object)
	pred := strings.ToLower(strings.ReplaceAll(predicate, " ", "_"))
	if ended == "" {
		ended = time.Now().Format("2006-01-02")
	}
	_, err := kg.db.Exec(
		"UPDATE triples SET valid_to=? WHERE subject=? AND predicate=? AND object=? AND valid_to IS NULL",
		ended, subID, pred, objID,
	)
	if err != nil {
		return fmt.Errorf("kg: invalidate: %w", err)
	}
	return nil
}

// QueryEntity returns facts about an entity.
func (kg *KG) QueryEntity(name string, opts QueryOpts) ([]Fact, error) {
	eid := EntityID(name)
	dir := opts.Direction
	if dir == "" {
		dir = "outgoing"
	}

	var results []Fact

	if dir == "outgoing" || dir == "both" {
		query := "SELECT t.*, e.name as obj_name FROM triples t JOIN entities e ON t.object = e.id WHERE t.subject = ?"
		params := []any{eid}
		if opts.AsOf != "" {
			query += " AND (t.valid_from IS NULL OR t.valid_from <= ?) AND (t.valid_to IS NULL OR t.valid_to >= ?)"
			params = append(params, opts.AsOf, opts.AsOf)
		}
		rows, err := kg.db.Query(query, params...)
		if err != nil {
			return nil, fmt.Errorf("kg: query outgoing: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFactOutgoing(rows, name)
			if err != nil {
				return nil, err
			}
			results = append(results, f)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	if dir == "incoming" || dir == "both" {
		query := "SELECT t.*, e.name as sub_name FROM triples t JOIN entities e ON t.subject = e.id WHERE t.object = ?"
		params := []any{eid}
		if opts.AsOf != "" {
			query += " AND (t.valid_from IS NULL OR t.valid_from <= ?) AND (t.valid_to IS NULL OR t.valid_to >= ?)"
			params = append(params, opts.AsOf, opts.AsOf)
		}
		rows, err := kg.db.Query(query, params...)
		if err != nil {
			return nil, fmt.Errorf("kg: query incoming: %w", err)
		}
		defer rows.Close()
		for rows.Next() {
			f, err := scanFactIncoming(rows, name)
			if err != nil {
				return nil, err
			}
			results = append(results, f)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	return results, nil
}

// QueryRelationship returns all triples with a given predicate.
func (kg *KG) QueryRelationship(predicate string, asOf string) ([]Fact, error) {
	pred := strings.ToLower(strings.ReplaceAll(predicate, " ", "_"))
	query := `
		SELECT t.*, s.name as sub_name, o.name as obj_name
		FROM triples t
		JOIN entities s ON t.subject = s.id
		JOIN entities o ON t.object = o.id
		WHERE t.predicate = ?
	`
	params := []any{pred}
	if asOf != "" {
		query += " AND (t.valid_from IS NULL OR t.valid_from <= ?) AND (t.valid_to IS NULL OR t.valid_to >= ?)"
		params = append(params, asOf, asOf)
	}
	rows, err := kg.db.Query(query, params...)
	if err != nil {
		return nil, fmt.Errorf("kg: query relationship: %w", err)
	}
	defer rows.Close()

	var results []Fact
	for rows.Next() {
		f, err := scanFactRelationship(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// Timeline returns facts in chronological order, optionally filtered by entity.
func (kg *KG) Timeline(entityName string) ([]Fact, error) {
	var rows *sql.Rows
	var err error

	if entityName != "" {
		eid := EntityID(entityName)
		rows, err = kg.db.Query(`
			SELECT t.*, s.name as sub_name, o.name as obj_name
			FROM triples t
			JOIN entities s ON t.subject = s.id
			JOIN entities o ON t.object = o.id
			WHERE (t.subject = ? OR t.object = ?)
			ORDER BY (t.valid_from IS NULL), t.valid_from ASC
			LIMIT 100
		`, eid, eid)
	} else {
		rows, err = kg.db.Query(`
			SELECT t.*, s.name as sub_name, o.name as obj_name
			FROM triples t
			JOIN entities s ON t.subject = s.id
			JOIN entities o ON t.object = o.id
			ORDER BY (t.valid_from IS NULL), t.valid_from ASC
			LIMIT 100
		`)
	}
	if err != nil {
		return nil, fmt.Errorf("kg: timeline: %w", err)
	}
	defer rows.Close()

	var results []Fact
	for rows.Next() {
		f, err := scanFactRelationship(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, f)
	}
	return results, rows.Err()
}

// Stats returns summary statistics.
func (kg *KG) Stats() (*KGStats, error) {
	var entities, triples, current int

	if err := kg.db.QueryRow("SELECT COUNT(*) FROM entities").Scan(&entities); err != nil {
		return nil, fmt.Errorf("kg: stats entities: %w", err)
	}
	if err := kg.db.QueryRow("SELECT COUNT(*) FROM triples").Scan(&triples); err != nil {
		return nil, fmt.Errorf("kg: stats triples: %w", err)
	}
	if err := kg.db.QueryRow("SELECT COUNT(*) FROM triples WHERE valid_to IS NULL").Scan(&current); err != nil {
		return nil, fmt.Errorf("kg: stats current: %w", err)
	}

	rows, err := kg.db.Query("SELECT DISTINCT predicate FROM triples ORDER BY predicate")
	if err != nil {
		return nil, fmt.Errorf("kg: stats predicates: %w", err)
	}
	defer rows.Close()

	var predicates []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		predicates = append(predicates, p)
	}

	return &KGStats{
		Entities:          entities,
		Triples:           triples,
		CurrentFacts:      current,
		ExpiredFacts:      triples - current,
		RelationshipTypes: predicates,
	}, rows.Err()
}

// scan helpers

func scanFactOutgoing(rows *sql.Rows, entityName string) (Fact, error) {
	var (
		id, subject, predicate, object string
		validFrom, validTo             sql.NullString
		confidence                     float64
		sourceCloset, sourceFile       sql.NullString
		extractedAt                    sql.NullString
		objName                        string
	)
	if err := rows.Scan(&id, &subject, &predicate, &object, &validFrom, &validTo, &confidence, &sourceCloset, &sourceFile, &extractedAt, &objName); err != nil {
		return Fact{}, fmt.Errorf("kg: scan outgoing: %w", err)
	}
	return Fact{
		Direction:    "outgoing",
		Subject:      entityName,
		Predicate:    predicate,
		Object:       objName,
		ValidFrom:    validFrom.String,
		ValidTo:      validTo.String,
		Confidence:   confidence,
		SourceCloset: sourceCloset.String,
		Current:      !validTo.Valid,
	}, nil
}

func scanFactIncoming(rows *sql.Rows, entityName string) (Fact, error) {
	var (
		id, subject, predicate, object string
		validFrom, validTo             sql.NullString
		confidence                     float64
		sourceCloset, sourceFile       sql.NullString
		extractedAt                    sql.NullString
		subName                        string
	)
	if err := rows.Scan(&id, &subject, &predicate, &object, &validFrom, &validTo, &confidence, &sourceCloset, &sourceFile, &extractedAt, &subName); err != nil {
		return Fact{}, fmt.Errorf("kg: scan incoming: %w", err)
	}
	return Fact{
		Direction:    "incoming",
		Subject:      subName,
		Predicate:    predicate,
		Object:       entityName,
		ValidFrom:    validFrom.String,
		ValidTo:      validTo.String,
		Confidence:   confidence,
		SourceCloset: sourceCloset.String,
		Current:      !validTo.Valid,
	}, nil
}

func scanFactRelationship(rows *sql.Rows) (Fact, error) {
	var (
		id, subject, predicate, object string
		validFrom, validTo             sql.NullString
		confidence                     float64
		sourceCloset, sourceFile       sql.NullString
		extractedAt                    sql.NullString
		subName, objName               string
	)
	if err := rows.Scan(&id, &subject, &predicate, &object, &validFrom, &validTo, &confidence, &sourceCloset, &sourceFile, &extractedAt, &subName, &objName); err != nil {
		return Fact{}, fmt.Errorf("kg: scan relationship: %w", err)
	}
	return Fact{
		Subject:      subName,
		Predicate:    predicate,
		Object:       objName,
		ValidFrom:    validFrom.String,
		ValidTo:      validTo.String,
		Confidence:   confidence,
		SourceCloset: sourceCloset.String,
		Current:      !validTo.Valid,
	}, nil
}

func nilStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
