---
description: Testing patterns — behavioral equivalence suite, table-driven, golden files
globs: "**/*_test.go"
---

# Testing

## Primary Strategy: Behavioral Equivalence

MemPalace Go is a Python→Go port. The authoritative test spec is `mempalace-go-test-suite-result.md` — a black-box behavioral equivalence suite that invokes both Python and Go implementations via subprocess and compares observable outputs (stdout, stderr, exit codes, files, SQLite contents).

- Tests do NOT exercise internal Go state — they drive the CLI and compare results.
- Python reference impl is the oracle. When outputs diverge, Go is wrong unless the test is on the non-deterministic skip list (embeddings, timestamps, UUIDs).
- Every behavior `B-NNN` in the suite doc has a corresponding test case.

## Stack

- `testing` stdlib for unit + harness.
- `os/exec` for subprocess invocation of both `mempalace` (Go) and `python -m mempalace` (Python).
- Golden files under `testdata/golden/B-NNN.{stdout,stderr,exit}` for deterministic fixtures.
- No mocks of sqlite-vec or embedders in integration tests. Unit tests may use a fake `Embedder` returning fixed vectors.

## Test Layers

| Layer | Scope | Example |
|-------|-------|---------|
| Unit | Pure functions (extractor scoring, gitignore matching, chunker, normalizer) | `extractor_test.go` |
| Integration | Package against real sqlite + fake embedder | `palace_test.go` |
| Behavioral | Full CLI subprocess vs Python oracle | `testsuite/harness_test.go` |

## Table-Driven Tests

```go
tests := []struct {
    name    string
    input   string
    want    []Memory
}{
    {"decision marker", "We decided to ship on Friday.", []Memory{{Type: Decision, ...}}},
    {"no signal", "hello world", nil},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        got := ExtractMemories(tt.input, 0.3)
        if !reflect.DeepEqual(got, tt.want) {
            t.Errorf("got %v, want %v", got, tt.want)
        }
    })
}
```

## Golden Files

- Store expected outputs under `testdata/golden/{behavior_id}.{stdout,stderr,exit}`.
- `go test -update` flag regenerates goldens from Python reference impl.
- Diff with `cmp.Diff` or `bytes.Equal`; on failure, print both sides.

## Non-Determinism Skip List

Not all behaviors can be bit-exact. Skip / fuzz-match these:
- **Embedding vectors**: compare cosine similarity ordering, not raw floats.
- **Timestamps** (`filed_at`, `source_mtime`): normalize to `<TIMESTAMP>` before diff.
- **UUIDs / paths**: substitute stable placeholders.
- **Map iteration order**: sort JSON output before compare.

## Determinism Anchors

- Drawer IDs are deterministic MD5 of `source_file + chunk_index` — compare exactly.
- Chunk boundaries are deterministic (800 chars, 100 overlap, paragraph-aware) — compare exactly.
- Room routing (folder > filename > keyword scoring) is deterministic — compare exactly.
- Gitignore matching must match Python behavior byte-for-byte.

## Coverage

- Target: >= 70% in domain packages (`palace/`, `miner/`, `extractor/`, `normalize/`, `entity/`).
- `make test/cover` gera report.
- Nao persiga 100% — foque em fluxos criticos e edge cases cobertos pelo behavioral suite.

## O Que Testar

- **Sempre:** Fluxos completos do CLI (init → mine → search).
- **Sempre:** Behavioral parity com Python para todo `B-NNN` P0/P1.
- **Sempre:** Edge cases de normalizacao (Claude Code JSONL, Codex, ChatGPT mapping tree).
- **Sempre:** Chunker boundary cases (exactly 800 chars, paragraph boundaries, min 50).
- **Skip:** Getters triviais, constructors, constantes.
