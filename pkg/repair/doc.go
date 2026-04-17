// Package repair audits palace health. It is the PUBLIC counterpart to
// internal/repair — the two packages cover different scopes:
//
//   - internal/repair: rebuilds a palace from the underlying source tree
//     (used by `mempalace repair` CLI for embedding changes and schema
//     migrations that need full re-ingest).
//   - pkg/repair: runs non-destructive audits over an OPEN palace — orphan
//     scan, WAL integrity check, embedding-dim mismatch detection — with
//     a report-only default and an opt-in auto-delete for orphans.
//
// pkg/repair is library-only for gp-5. CLI/MCP surfacing is deferred.
package repair
