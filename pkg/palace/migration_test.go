package palace_test

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	vec "github.com/asg017/sqlite-vec-go-bindings/cgo"
	_ "github.com/mattn/go-sqlite3"

	"github.com/guerra/go-palace/pkg/embed"
	"github.com/guerra/go-palace/pkg/palace"
)

// v0.1.0 schema — copied verbatim (NOT imported) so a later change to
// pkg/palace/schema.go cannot silently break this migration fixture.
// Notable: no hall column on drawers; no hall partition on drawers_vec;
// no schema_version key.
const v01SchemaDrawers = `CREATE TABLE drawers (
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
)`

const v01SchemaMeta = `CREATE TABLE palace_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
)`

// buildV01Fixture creates a raw v0.1.0-shape DB at path with `rows` drawers
// pre-inserted (deterministic embeddings via FakeEmbedder). Returns the
// embedder so callers can reopen with matching dim.
func buildV01Fixture(t *testing.T, path string, rows []palace.Drawer) *embed.FakeEmbedder {
	t.Helper()
	e := embed.NewFakeEmbedder(palace.DefaultEmbeddingDim)

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		t.Fatalf("WAL: %v", err)
	}
	if _, err := db.Exec(v01SchemaDrawers); err != nil {
		t.Fatalf("DDL drawers: %v", err)
	}
	if _, err := db.Exec(v01SchemaMeta); err != nil {
		t.Fatalf("DDL meta: %v", err)
	}
	vecDDL := fmt.Sprintf(`CREATE VIRTUAL TABLE drawers_vec USING vec0(
        id text primary key,
        wing text partition key,
        room text partition key,
        source_file text partition key,
        embedding float[%d]
    )`, palace.DefaultEmbeddingDim)
	if _, err := db.Exec(vecDDL); err != nil {
		t.Fatalf("DDL vec: %v", err)
	}
	// embedding_dim is written — this triggers the fast-path dim check.
	if _, err := db.Exec(
		`INSERT INTO palace_meta (key, value) VALUES ('embedding_dim', ?)`,
		fmt.Sprintf("%d", palace.DefaultEmbeddingDim),
	); err != nil {
		t.Fatalf("write dim: %v", err)
	}

	for _, d := range rows {
		if _, err := db.Exec(
			`INSERT INTO drawers (id, document, wing, room, source_file, chunk_index,
                added_by, filed_at, source_mtime, metadata_json)
             VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			d.ID, d.Document, d.Wing, d.Room, d.SourceFile, d.ChunkIndex,
			d.AddedBy, d.FiledAt.Format("2006-01-02T15:04:05Z07:00"),
			d.SourceMTime, metadataJSON(t, d.Metadata),
		); err != nil {
			t.Fatalf("insert drawer: %v", err)
		}
		// Embed and insert vec row under the OLD (wing, room, source_file) partition.
		vecs, err := e.Embed([]string{d.Document})
		if err != nil {
			t.Fatalf("embed: %v", err)
		}
		blob, err := vec.SerializeFloat32(vecs[0])
		if err != nil {
			t.Fatalf("serialize: %v", err)
		}
		if _, err := db.Exec(
			`INSERT INTO drawers_vec (id, wing, room, source_file, embedding)
             VALUES (?, ?, ?, ?, ?)`,
			d.ID, d.Wing, d.Room, d.SourceFile, blob,
		); err != nil {
			t.Fatalf("insert vec: %v", err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	return e
}

func metadataJSON(t *testing.T, md map[string]any) string {
	t.Helper()
	if md == nil {
		return "{}"
	}
	b, err := json.Marshal(md)
	if err != nil {
		t.Fatalf("marshal metadata: %v", err)
	}
	return string(b)
}

func TestMigrationFromV1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	fixtures := []palace.Drawer{
		{
			ID:         palace.ComputeDrawerID("proj", "docs", "a.md", 0),
			Document:   "knowledge about Go",
			Wing:       "proj",
			Room:       "docs",
			SourceFile: "a.md",
			ChunkIndex: 0,
			AddedBy:    "miner",
		},
		{
			ID:         palace.ComputeDrawerID("proj", "diary", "b.md", 0),
			Document:   "today I learned",
			Wing:       "proj",
			Room:       "diary",
			SourceFile: "b.md",
			ChunkIndex: 0,
			AddedBy:    "diary_user",
			Metadata:   map[string]any{"hall": "hall_diary"},
		},
		{
			ID:         palace.ComputeDrawerID("proj", "convos", "c.md", 0),
			Document:   "user: hi / assistant: hello",
			Wing:       "proj",
			Room:       "convos",
			SourceFile: "c.md",
			ChunkIndex: 0,
			AddedBy:    "convominer",
			Metadata:   map[string]any{"ingest_mode": "convos"},
		},
	}
	_ = buildV01Fixture(t, path, fixtures)

	// Open with v0.2.0 binary — triggers migration.
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open post-migration: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// (a) row count preserved
	n, err := p.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != len(fixtures) {
		t.Errorf("row count: got %d want %d", n, len(fixtures))
	}

	// (b) halls backfilled correctly
	got, err := p.Get(palace.GetOptions{Limit: 10})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	halls := make(map[string]string)
	for _, d := range got {
		halls[d.SourceFile] = d.Hall
	}
	if halls["a.md"] != "knowledge" {
		t.Errorf("a.md hall: got %q want knowledge", halls["a.md"])
	}
	if halls["b.md"] != "diary" {
		t.Errorf("b.md hall: got %q want diary", halls["b.md"])
	}
	if halls["c.md"] != "conversations" {
		t.Errorf("c.md hall: got %q want conversations", halls["c.md"])
	}

	// (c) Query works — vec rebuild succeeded
	res, err := p.Query("knowledge about Go", palace.QueryOptions{NResults: 3})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) == 0 {
		t.Fatal("query returned no results post-migration")
	}

	// (d) backup file exists
	if _, err := os.Stat(path + ".pre-v0.2.bak"); err != nil {
		t.Errorf("backup file missing: %v", err)
	}

	// (e) entities table exists (v3 additive migration path) — EntityList
	// must succeed without error even on a freshly-migrated v1 palace.
	if _, err := p.EntityList(); err != nil {
		t.Errorf("v3 entities table missing post-migration: %v", err)
	}
}

// TestMigrationV2ToV3Additive exercises the v2→v3 additive path: a palace
// that already has the v2 schema (drawers + drawers_vec with hall partition)
// but no entities table must gain the table on the next Open() without any
// destructive work (no backup, no vec rebuild).
func TestMigrationV2ToV3Additive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")

	// Step 1: open with the current binary to produce a v3 palace, add a
	// drawer, then manually rewind schema_version to 2 and DROP the
	// entities table. This simulates a v2 palace persisted before v3.
	p1, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open 1: %v", err)
	}
	if err := p1.Upsert(palace.Drawer{
		ID:         palace.ComputeDrawerID("w", "r", "a.md", 0),
		Document:   "hello",
		Wing:       "w",
		Hall:       "knowledge",
		Room:       "r",
		SourceFile: "a.md",
		ChunkIndex: 0,
		AddedBy:    "test",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	_ = p1.Close()

	// Forcibly regress the schema to v2 shape.
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS entities`); err != nil {
		t.Fatalf("drop entities: %v", err)
	}
	if _, err := db.Exec(
		`INSERT OR REPLACE INTO palace_meta (key, value) VALUES ('schema_version', '2')`,
	); err != nil {
		t.Fatalf("rewind schema_version: %v", err)
	}
	_ = db.Close()

	// Step 2: re-open with the current binary — v2 → v3 additive migration.
	p2, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open 2 (v2→v3): %v", err)
	}
	t.Cleanup(func() { _ = p2.Close() })

	// (a) entities table exists again.
	if _, err := p2.EntityList(); err != nil {
		t.Errorf("entities table missing after v2→v3: %v", err)
	}

	// (b) pre-existing drawer survived.
	n, err := p2.Count()
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 1 {
		t.Errorf("drawer count after v2→v3: got %d want 1", n)
	}

	// (c) no backup file was created (additive path must not touch it).
	if _, err := os.Stat(path + ".pre-v0.2.bak"); err == nil {
		t.Error("v2→v3 additive path created a backup — should not have")
	}
}

func TestMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	fixtures := []palace.Drawer{
		{
			ID:         palace.ComputeDrawerID("w", "r", "a.md", 0),
			Document:   "hello",
			Wing:       "w",
			Room:       "r",
			SourceFile: "a.md",
			ChunkIndex: 0,
			AddedBy:    "test",
		},
	}
	_ = buildV01Fixture(t, path, fixtures)

	// First open triggers migration.
	p1, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open1: %v", err)
	}
	_ = p1.Close()

	// Backup file should exist; remember its mtime to detect re-creation.
	backupPath := path + ".pre-v0.2.bak"
	info1, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("backup missing after first open: %v", err)
	}

	// Second open should be a no-op (schema_version gate).
	p2, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open2: %v", err)
	}
	_ = p2.Close()

	// If gate failed and migration re-ran, a new backup would be .pre-v0.2.bak.1
	if _, err := os.Stat(backupPath + ".1"); err == nil {
		t.Error("second open re-ran migration (found .bak.1)")
	}

	info2, err := os.Stat(backupPath)
	if err != nil {
		t.Fatalf("backup disappeared: %v", err)
	}
	if !info1.ModTime().Equal(info2.ModTime()) {
		t.Error("backup file was rewritten on second open")
	}
}

func TestMigrationEmptyPalace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	// Fresh palace — no fixture build.
	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	_ = p.Close()

	// No backup should be created for a fresh palace (nothing to migrate).
	if _, err := os.Stat(path + ".pre-v0.2.bak"); err == nil {
		t.Error("fresh palace created backup (should skip migrateToV2)")
	}
}

func TestMigrationPreservesVectorSearch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	fixtures := []palace.Drawer{
		{
			ID:       palace.ComputeDrawerID("w", "r", "a.md", 0),
			Document: "alpha",
			Wing:     "w", Room: "r", SourceFile: "a.md", ChunkIndex: 0,
			AddedBy: "t",
		},
		{
			ID:       palace.ComputeDrawerID("w", "r", "b.md", 0),
			Document: "beta",
			Wing:     "w", Room: "r", SourceFile: "b.md", ChunkIndex: 0,
			AddedBy: "t",
		},
	}
	_ = buildV01Fixture(t, path, fixtures)

	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// Query for "alpha" — FakeEmbedder is deterministic so top-1 should be a.md.
	res, err := p.Query("alpha", palace.QueryOptions{NResults: 1})
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 result, got %d", len(res))
	}
	if res[0].Drawer.SourceFile != "a.md" {
		t.Errorf("top-1 post-migration: got %q want a.md", res[0].Drawer.SourceFile)
	}
}

func TestMigrationBackfillsDiary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	fixtures := []palace.Drawer{
		{
			ID:       palace.ComputeDrawerID("w", "r", "x.md", 0),
			Document: "diary-ish",
			Wing:     "w", Room: "r", SourceFile: "x.md", ChunkIndex: 0,
			AddedBy:  "t",
			Metadata: map[string]any{"hall": "hall_diary"},
		},
	}
	_ = buildV01Fixture(t, path, fixtures)

	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	got, err := p.Get(palace.GetOptions{Limit: 10})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Hall != "diary" {
		t.Errorf("backfill: got %+v want Hall=diary", got)
	}
}

// TestMigrationLegacyEmptyPalace covers the init-but-never-mined upgrade path:
// a v0.1.0 palace where `drawers` and `drawers_vec` exist with the OLD shape
// but zero rows. The migration MUST rebuild `drawers_vec` with the new
// (wing, hall, room, source_file) partition schema — otherwise the first write
// after upgrade fails because `upsertOne` inserts into the `hall` column which
// the legacy vec table does not have.
func TestMigrationLegacyEmptyPalace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	// Build a v0.1 fixture with zero drawers — tables exist, rows do not.
	_ = buildV01Fixture(t, path, nil)

	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	// First write after migration must succeed — i.e. drawers_vec has the new
	// schema including the `hall` partition.
	d := palace.Drawer{
		ID:         palace.ComputeDrawerID("w", "r", "fresh.md", 0),
		Document:   "post-upgrade content",
		Wing:       "w",
		Hall:       "knowledge",
		Room:       "r",
		SourceFile: "fresh.md",
		ChunkIndex: 0,
		AddedBy:    "test",
	}
	if err := p.Upsert(d); err != nil {
		t.Fatalf("upsert after legacy-empty migration: %v", err)
	}
	got, err := p.Get(palace.GetOptions{Where: map[string]string{"source_file": "fresh.md"}})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Hall != "knowledge" {
		t.Errorf("expected 1 drawer with Hall=knowledge, got %+v", got)
	}
	// Backup SHOULD be created because this is a legacy (not fresh) palace.
	if _, err := os.Stat(path + ".pre-v0.2.bak"); err != nil {
		t.Errorf("expected backup for legacy-empty palace: %v", err)
	}
}

func TestMigrationBackfillsConvos(t *testing.T) {
	path := filepath.Join(t.TempDir(), "p.db")
	fixtures := []palace.Drawer{
		{
			ID:       palace.ComputeDrawerID("w", "r", "y.md", 0),
			Document: "chat",
			Wing:     "w", Room: "r", SourceFile: "y.md", ChunkIndex: 0,
			AddedBy:  "t",
			Metadata: map[string]any{"ingest_mode": "convos"},
		},
	}
	_ = buildV01Fixture(t, path, fixtures)

	p, err := palace.Open(path, embed.NewFakeEmbedder(palace.DefaultEmbeddingDim))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = p.Close() })

	got, err := p.Get(palace.GetOptions{Limit: 10})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(got) != 1 || got[0].Hall != "conversations" {
		t.Errorf("backfill: got %+v want Hall=conversations", got)
	}
}
