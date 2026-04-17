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

### Changed
- `drawers` table: new `hall TEXT NOT NULL DEFAULT ''` column.
- `drawers_vec` virtual table: partition keys are now `(wing, hall, room, source_file)` — previously `(wing, room, source_file)`.
- `palace.Drawer` struct gains `Hall` field (inserted between `Wing` and `Room`); `palace.QueryOptions` gains `Hall` filter in the same position.
- Consumers (`internal/miner`, `internal/convominer`, `mcp` `diary_write` and `add_drawer`, `benchmarks/longmemeval`) now populate `Drawer.Hall`.
- `palace.Palace.UpsertBatch` now normalizes documents via `normalize.Normalize` before calling the embedder. Stored `drawers.document` remains the raw caller-supplied string (dual-state: stored raw, embedded normalized). Existing drawers are unaffected unless re-embedded.
- `mempalace dedup --threshold` now takes cosine **SIMILARITY** (default 0.95) instead of cosine **DISTANCE** (default 0.15). Users passing `--threshold 0.15` must update to `--threshold 0.85` for the equivalent strictness. A runtime warning fires when `--threshold < 0.5` to catch stale invocations.
- `mempalace dedup` gains `--hall <name>` and `--merge` flags. `--merge` collapses loser metadata into the surviving (longest) drawer via `palace.MergeAndDelete`.

### Removed
- `internal/dedup` package — lifted to `pkg/dedup`. CLI callers migrated.

### Breaking
- Existing v0.1.0 databases auto-migrate on first `palace.Open()` with the v0.2.0 binary. A file-copy backup is created at `<path>.pre-v0.2.bak` before destructive vec-table rebuild. Re-embedding every drawer is O(N) embedder calls — expensive on large palaces.
- Callers using `palace.Drawer` struct literals are source-compatible (all in-tree callers use keyed literals), but any external code using positional initialization will break.
- `palace_meta` table now carries a `schema_version` key. External readers should treat it as opaque.
- `mempalace dedup --threshold` semantic flip (similarity, not distance) — see Changed above.
