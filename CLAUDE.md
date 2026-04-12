# MemPalace Go

Port of Python [MemPalace](https://github.com/) to Go. Local-first memory palace for AI agents: project/conversation mining, semantic search via sqlite-vec, 4-layer memory stack, knowledge graph, and MCP server.

**Source of truth:**
- `mempalace-go-plan-result.md` — full migration plan (behavioral contracts, Go architecture, package layout, ADRs)
- `mempalace-go-test-suite-result.md` — black-box behavioral equivalence suite (Python oracle ↔ Go)
- Python reference impl at `~/projects/pessoal/mempalace-ref/mempalace/`

## Stack

| Role | Choice |
|------|--------|
| Language | Go 1.24+ |
| CLI framework | `spf13/cobra` |
| Vector store | `sqlite-vec` (CGO) |
| SQL driver | `mattn/go-sqlite3` (CGO) |
| Config | JSON (`config.json`) + YAML (`mempalace.yaml`, `gopkg.in/yaml.v3`) |
| Embeddings | hugot (pure-Go, offline) — HugotEmbedder default, FakeEmbedder for tests |
| MCP | Hand-rolled JSON-RPC over stdio |
| Logging | `log/slog` |

## Commands

```bash
make audit          # Full pipeline: tidy + vet + lint + test + build
make build          # Build binary to bin/mempalace (CGO_ENABLED=1)
make run ARGS=...   # go run ./cmd/mempalace <args>
make test           # Unit + integration tests
make test/race      # Tests with race detector
make test/cover     # Tests with coverage report (coverage.out)
make test/suite     # Behavioral equivalence suite vs Python oracle (TODO)
make test/semantic  # Semantic search quality tests (real embeddings, requires model)
make lint           # golangci-lint run ./...
make vet            # go vet ./...
make tidy           # go mod tidy
make help           # Show all targets
```

## Validation Pipeline

Ordered — each step must pass before the next:

1. `go mod tidy` — deps clean
2. `go vet ./...` — stdlib static checks
3. `golangci-lint run ./...` — errcheck, staticcheck, revive, goimports, misspell, etc.
4. `go test ./...` — unit + integration
5. `go build -o bin/mempalace ./cmd/mempalace` — compile with CGO

One command runs it all: `make audit`.

## Architecture

```
cmd/mempalace/        # CLI entry (cobra dispatch)
pkg/                  # Public API — importable by external consumers
  config/             # JSON config + env vars
  palace/             # sqlite-vec store (replaces ChromaDB)
  embed/              # Embedder interface + impls (Hugot, Python subprocess, Fake)
  searcher/           # Semantic search
  layers/             # L0/L1/L2/L3 memory stack
  kg/                 # Knowledge graph (SQLite)
  sanitizer/          # Input sanitization
internal/             # Private — not importable outside this module
  miner/              # Project file mining (worker pool)
  convominer/         # Conversation mining
  normalize/          # Chat format normalization (Claude/Codex/ChatGPT/Slack)
  extractor/          # 5-type heuristic memory extraction
  dialect/            # AAAK dialect encoder/decoder
  graph/              # Palace graph traversal
  entity/             # Entity detection + registry
  room/               # Room detection from folders
  spellcheck/         # Spell correction
  splitter/           # Mega-file splitting
  hooks/              # Harness hook handlers
  dedup/              # Duplicate detection
  repair/             # Palace repair utilities
  instructions/       # Instruction handling
mcp/                  # MCP server (public)
version/              # Version constant
```

Package design details in `mempalace-go-plan-result.md` Phase 2.

> Full module map, data flows, ADRs, and scaling assessment: `.agents/arch/system.arch.md`

## Rules

Project rules live in `.claude/rules/`. Mandatory rules load automatically; path-scoped rules load when you touch matching files.

| Rule | Type | Triggers | Domain |
|------|------|----------|--------|
| `principles.md` | mandatory | always | KISS, fail-fast, density, single-responsibility |
| `makefile.md` | mandatory | always | Make target conventions |
| `go-patterns.md` | path-scoped | `**/*.go` | Structure, interfaces, DI, errors, config, SQL safety |
| `go-concurrency.md` | path-scoped | `**/*.go` | Context scopes, errgroup, worker pools, shutdown |
| `testing.md` | path-scoped | `**/*_test.go` | Behavioral equivalence suite, golden files, determinism |

## Reference Projects

- `~/projects/pessoal/meu-condominio/` — Go backend reference (stack + rule source)
- `~/projects/pessoal/sinnema/` — Go backend reference (puro engine pattern)
