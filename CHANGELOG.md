# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-04-17

### Added
- `pkg/halls` package introducing the 4th hierarchy tier: wing > hall > room > drawer. Exports the six canonical halls (`conversations`, `journal`, `diary`, `knowledge`, `tasks`, `scratch`), an `All` slice, `IsValid`, and `Detect(content, room, addedBy, metadata)` heuristic classifier.
- `Drawer.Hall string` field and `QueryOptions.Hall` filter on `pkg/palace`. `"hall"` is now an allowed key in `GetOptions.Where`.
- Automatic schema migration on first `palace.Open()` of a v0.1.0 database: backs up the file to `<path>.pre-v0.2.bak` (WAL file copied when present), adds the `hall` column, backfills via `halls.Detect`, rebuilds `drawers_vec` with the new partition schema, and re-embeds every drawer.
- `schema_version` key in `palace_meta` gates migration so subsequent opens are no-ops.
- `ErrSchemaOutdated` sentinel for migration failures.
- `mempalace_add_drawer` MCP tool now accepts an optional `hall` argument.
- `pkg/normalize` package: pre-embed content normalization exposing `Normalize` (whitespace collapse, NFC, invalid-UTF-8 substitution). Stdlib + `golang.org/x/text/unicode/norm`. Used internally by `palace.UpsertBatch`.
- `pkg/dedup` package: cosine-similarity duplicate detection with hall-aware `(wing, hall, source_file)` partition, optional metadata merge, dry-run mode. Exposes `Run(p, opts)`, `DedupOptions`, `DedupReport`, `DuplicateGroup`, `ErrInvalidThreshold`.
- `palace.Palace.MergeAndDelete(winnerID, loserIDs, mergedMeta)` atomic primitive for dedup engines. Enforces the `(wing, hall, source_file)` partition guard inside a single transaction.
- `palace.ErrDedupCrossPartition` sentinel error.
- `pkg/entity` package: per-content typed entity detection (`person`, `place`,
  `project`, `tool`, `date`, `url`, `email`, `uncertain`) with byte offsets,
  plus a pluggable `Registry` (Add / Lookup / Merge / List / ListByType)
  backed by either an in-memory store (default) or a palace-backed sqlite
  store. Ports the Python `entity_detector.py` person + project regex
  patterns verbatim and extends with Go-authored patterns for the remaining
  types. Pure Go, no ML, no network.
- `palace.EntityRow` struct + `palace.Palace.EntityUpsert`,
  `palace.Palace.EntityList`, `palace.Palace.EntityDelete` narrow primitives
  backing `pkg/entity.PalaceStore`.
- `palace.ErrEntityNotFound` sentinel error.
- `entities` table added to the palace schema. `schema_version` bumped
  from 2 to 3. v2 palaces auto-migrate additively on next `palace.Open()`
  (no vec rebuild, no data transform, no backup).
- `pkg/extractor` package: 5-type heuristic classifier (decision,
  preference, milestone, problem, emotion) ported from Python
  `general_extractor.py`. Exposes `Extract(content) []Classification`,
  `ExtractWith(content, opts)`, `ExtractSegments(content, opts) []Segment`,
  and `Classify(segment, opts)`. Pure Go, no ML, no network. Marker
  regex sets ported verbatim from Python; confidence formula and
  disambiguation rules match the oracle.
- `palace.PalaceOptions` struct + `palace.OpenWithOptions(path, emb, opts)`
  constructor. `DefaultPalaceOptions()` returns
  `PalaceOptions{ExtractOnUpsert: true}`, so the existing `Open` path
  gains automatic classification on every Upsert / UpsertBatch:
  classifications are written under `Drawer.Metadata["classifications"]`
  as `[]extractor.Classification`. Opt out via `OpenWithOptions`.
- `palace.QueryOptions.Classification` filter: semantic search can now be
  restricted to drawers whose classifications include a specific type.
  Backed by `json_each` / `json_extract` over `drawers.metadata_json`.
- `pkg/kg/extract.go` exports `AutoExtractTriples(drawer, entities) []palace.TripleRow`
  and a `VerbPatterns` table of 6 heuristic verb families
  (`works_at`, `lives_in`, `uses`, `prefers`, `started`, `finished`) anchored
  on detected entities with configurable subject/object type allowlists.
  Emits triples with `DefaultExtractConfidence = 0.6` and
  `ValidFrom = drawer.FiledAt` (YYYY-MM-DD). English-only MVP; non-English
  surface forms are out of scope.
- `kg.NewPalaceAdapter(k)` returning a `palace.TripleSink` adapter for
  wiring pkg/kg into palace without creating an import cycle.
- `kg.NewStatelessEntityDetector()` convenience `palace.EntityDetector`
  that wraps `entity.Detect` as a pure function (no shared state).
- `palace.TripleSink` interface (`AddTriple(TripleRow) (string, error)`)
  and `palace.TripleRow` struct. Defined on the palace side so
  `pkg/palace` imports nothing from `pkg/kg` (cycle-break).
- `palace.EntityDetector` interface (`DetectEntities(content) []EntityMatch`)
  and `palace.EntityMatch` struct. Mirrors pkg/entity fields needed for
  triple extraction without palace importing pkg/entity.
- `palace.PalaceOptions` gains `AutoExtractKG`, `KG`, `EntityRegistry`,
  `AutoExtractFn`, and `TrackLastAccessed` fields. All default to the
  zero value so existing behavior is unchanged (opt-in only).
- `(*palace.Palace).TouchLastAccessed(ids)` — batch UPDATE of
  `metadata_json.$.last_accessed` via `json_set`. Used by `Query` when
  `TrackLastAccessed=true` and by tests.
- `(*palace.Palace).ColdDrawerIDs(before, limit, protectedHalls)` —
  selects ids where `COALESCE(metadata_json.$.last_accessed, filed_at) < ?`
  with hall-based exclusion.
- `(*palace.Palace).ArchiveDrawers(ids)` — UPDATE hall to
  `halls.HallArchived`. Vec partition hall is intentionally not rewritten;
  `Query` compensates by filtering on `d.hall != 'archived'` so archived
  drawers stop surfacing in semantic search unconditionally. `Get` still
  returns archived rows (bypasses the vec table).
- `(*palace.Palace).DeleteBatch(ids)` — atomic batched DELETE across
  drawers + drawers_vec in a single transaction. Mirrors `ArchiveDrawers`
  shape; used by `Compact(Action=ActionDelete)` so delete and archive
  paths share the same O(batch) fsync profile.
- `(*palace.Palace).IntegrityCheck()` / `QuickCheck()` — runs
  `PRAGMA integrity_check` / `quick_check`, returns rows != `"ok"`.
- `(*palace.Palace).ScanOrphans()` — returns drawer-without-vec and
  vec-without-drawer id lists from a read-tx snapshot.
- `(*palace.Palace).DeleteOrphans(drawerOrphans, vecOrphans)` — atomic
  DELETE with in-tx re-verification to avoid WAL races.
- `(*palace.Palace).EmbeddingDim()` / `ProbeEmbedderDim()` — typed
  accessors for stored-vs-embedder dim comparison.
- `pkg/palace/compact.go` exports `Compact`, `CompactOptions`,
  `CompactReport`, `CompactAction`. Defaults: `ColdDays=30`,
  `Action=ActionArchive`, `ProtectedHalls=[HallDiary, HallKnowledge]`,
  `MaxBatch=1000`. Action supports `ActionArchive` (hall='archived') and
  `ActionDelete` (removes drawers + vec rows).
- `pkg/repair` NEW package exporting `Repair`, `RepairOptions`,
  `RepairReport`, `RepairMode`, `DimInfo`. Orchestrates integrity check
  (skippable), orphan scan, and dim probe. `ModeReportOnly` (default) is
  read-only; `ModeAutoDelete` removes orphans only (integrity/dim issues
  stay read-only).
- `halls.HallArchived = "archived"` constant. Intentionally NOT in
  `halls.All` and `halls.IsValid` returns false for it — archive is a
  lifecycle state, not a canonical hall.

### Changed
- `drawers` table: new `hall TEXT NOT NULL DEFAULT ''` column.
- `drawers_vec` virtual table: partition keys are now `(wing, hall, room, source_file)` — previously `(wing, room, source_file)`.
- `palace.Drawer` struct gains `Hall` field (inserted between `Wing` and `Room`); `palace.QueryOptions` gains `Hall` filter in the same position.
- Consumers (`internal/miner`, `internal/convominer`, `mcp` `diary_write` and `add_drawer`, `benchmarks/longmemeval`) now populate `Drawer.Hall`.
- `palace.Palace.UpsertBatch` now normalizes documents via `normalize.Normalize` before calling the embedder. Stored `drawers.document` remains the raw caller-supplied string (dual-state: stored raw, embedded normalized). Existing drawers are unaffected unless re-embedded.
- `mempalace dedup --threshold` now takes cosine **SIMILARITY** (default 0.95) instead of cosine **DISTANCE** (default 0.15). Users passing `--threshold 0.15` must update to `--threshold 0.85` for the equivalent strictness. A runtime warning fires when `--threshold < 0.5` to catch stale invocations.
- `mempalace dedup` gains `--hall <name>` and `--merge` flags. `--merge` collapses loser metadata into the surviving (longest) drawer via `palace.MergeAndDelete`.
- `palace.Upsert` / `palace.UpsertBatch` may mutate `Drawer.Metadata` in
  place when `PalaceOptions.ExtractOnUpsert` is enabled (adds the
  `classifications` key; all other keys are preserved). Callers sensitive
  to in-place mutation pass deep-copied drawers.
- `internal/convominer` migrates from `internal/extractor.ExtractMemories`
  to `pkg/extractor.ExtractSegments`. Convo chunks classified with type
  "emotional" previously are now typed "emotion" (see Breaking).
- `palace.Query` can now bump `metadata_json.$.last_accessed` on every
  returned drawer id when `PalaceOptions.TrackLastAccessed=true`. OFF by
  default to preserve v0.1 read-only Query semantics; required for
  `Compact` to use last-access cold detection (Compact falls back to
  `filed_at` when the option is off). Bump failures are logged and
  ignored — they never fail a Query.
- `palace.UpsertBatch` now invokes `PalaceOptions.AutoExtractFn` after a
  successful tx commit when `AutoExtractKG`, `KG`, `EntityRegistry`, and
  `AutoExtractFn` are all set. Triple write failures are logged and do
  NOT roll back the palace write (palace is source of truth; KG is
  opportunistic).
- **v0.2.0 is now feature-complete** — gp-1 (halls) + gp-2
  (normalize+dedup) + gp-3 (entity) + gp-4 (extractor) + gp-5
  (kg-extract, compact, pkg/repair).

### Removed
- `internal/dedup` package — lifted to `pkg/dedup`. CLI callers migrated.
- `internal/extractor` package — lifted to `pkg/extractor`. All
  internal consumers migrated (`internal/convominer`).

### Breaking
- Existing v0.1.0 databases auto-migrate on first `palace.Open()` with the v0.2.0 binary. A file-copy backup is created at `<path>.pre-v0.2.bak` before destructive vec-table rebuild. Re-embedding every drawer is O(N) embedder calls — expensive on large palaces.
- Callers using `palace.Drawer` struct literals are source-compatible (all in-tree callers use keyed literals), but any external code using positional initialization will break.
- `palace_meta` table now carries a `schema_version` key. External readers should treat it as opaque.
- `mempalace dedup --threshold` semantic flip (similarity, not distance) — see Changed above.
- The `"emotional"` memory-type string is renamed to `"emotion"` on every
  Go API boundary: the public `pkg/extractor` type constant
  (`TypeEmotion = "emotion"`), the `Chunk.MemoryType` string produced by
  `convominer` in `general` mode, the `classifications` entries written
  to drawer metadata, AND the drawer `room` value for
  emotional-classified convominer drawers (convominer sets
  `room = chunk.MemoryType` in general mode). The Python oracle retains
  `"emotional"`. Drawers mined prior to gp-4 keep whatever strings were
  originally written (including `room = "emotional"`). Consumers
  filtering `room == "emotional"` against a palace with a mix of pre-gp-4
  and post-gp-4 drawers must broaden the filter (e.g.,
  `room IN ("emotional", "emotion")`) or re-mine. No automatic DB-level
  rename is performed.
- `palace.Open` now implicitly enables auto-classification on every
  `Upsert` / `UpsertBatch` (via `DefaultPalaceOptions().ExtractOnUpsert =
  true`). Existing callers see a new `classifications` key appear in
  `Drawer.Metadata` after the next Upsert. Callers iterating metadata
  keys must tolerate unknown keys (the type is already `map[string]any`).
  Opt out via `palace.OpenWithOptions(..., palace.PalaceOptions{
  ExtractOnUpsert: false})`.
