# go-palace

Local-first memory palace for AI agents. Mine project files and conversations into a semantic store, search with vector similarity, and serve memories over MCP.

<!-- badges: go version, tests, license -->

## What it does

- **Mine** project files and conversation logs into structured memories with automatic room/wing classification
- **Search** semantically using sqlite-vec vector embeddings (offline, no API calls)
- **Stack** memories in 4 layers: identity, essential story, on-demand, deep search
- **Serve** 19 MCP tools for Claude Desktop, picoclaw, or any MCP-compatible client
- **Graph** knowledge as subject-predicate-object triples with timeline queries

## Quick start

```bash
# Build (requires Go 1.26+, CGO for sqlite-vec)
make build

# Initialize a project — detects rooms from folder structure
bin/mempalace init ./my-project

# Mine project files into the palace
bin/mempalace mine ./my-project

# Search your memories
bin/mempalace search "authentication flow"

# Get a wake-up summary (L0 + L1 memory stack)
bin/mempalace wake-up
```

## Architecture

```
cmd/mempalace/          CLI entry (cobra)
pkg/                    Public API — importable by consumers
  palace/               sqlite-vec vector store
  config/               JSON config + env vars
  embed/                Embedder interface (Hugot, Fake)
  searcher/             Semantic search
  layers/               L0-L3 memory stack
  kg/                   Knowledge graph (subject-predicate-object)
  entity/               Entity detection + registry
  extractor/            5-type heuristic classifier (decision / preference / milestone / problem / emotion) — pure-Go port of Python general_extractor.py
  sanitizer/            Input sanitization
internal/               Private implementation
  miner/                Project file mining (worker pool, gitignore)
  convominer/           Conversation mining
  normalize/            Chat format normalization (Claude/Codex/ChatGPT/Slack)
  dialect/              AAAK dialect encoder/decoder
  graph/                Palace graph traversal
  entity/               Entity detection + registry
  room/                 Room detection from folders
  spellcheck/           Spell correction
  splitter/             Mega-file splitting
  hooks/                Harness hook handlers
  dedup/                Duplicate detection
  repair/               Palace repair utilities
  instructions/         Instruction handling
mcp/                    MCP JSON-RPC server over stdio
version/                Version constant
```

## Library usage

go-palace is designed to be imported as a library. The `pkg/` packages are the public API:

```go
import (
    "go-palace/pkg/palace"
    "go-palace/pkg/embed"
    "go-palace/pkg/searcher"
    "go-palace/pkg/layers"
    "go-palace/pkg/kg"
)

// Open a palace with the default hugot embedder
emb, cleanup, err := embed.NewHugotEmbedder("/path/to/model")
defer cleanup()

p, err := palace.Open("/path/to/palace.db", emb)
defer p.Close()

// Add a memory
err = p.Upsert(palace.Drawer{
    ID:       "my-drawer-1",
    Document: "The auth module uses JWT tokens with 24h expiry.",
    Wing:     "technical",
    Room:     "backend",
})

// Search
results, err := searcher.Search(p, "authentication tokens", 5)

// Wake-up (L0 + L1 summary)
stack := layers.NewStack(p, configDir)
text, err := stack.WakeUp()
```

## CLI commands

| Command | Description |
|---------|-------------|
| `status` | Print palace stats: drawers, wings, rooms |
| `init [dir]` | Detect rooms from folder structure, write `mempalace.yaml` |
| `mine [path]` | Mine project files or conversations into the palace |
| `search [query]` | Semantic search across all memories |
| `wake-up` | Generate L0+L1 wake-up text for agent context |
| `split [dir]` | Split mega-files (>300 LOC) into focused modules |
| `hook run` | Run a harness hook (reads JSON from stdin) |
| `instructions [name]` | Print built-in instruction sets |
| `repair` | Repair palace: fix orphaned drawers, rebuild indexes |
| `dedup` | Find and remove duplicate or near-duplicate drawers |
| `compress` | Compress drawers using AAAK dialect encoding |
| `mcp` | Start MCP server (JSON-RPC over stdio) |

### Mine modes

```bash
# Mine project files (default)
bin/mempalace mine ./my-project

# Mine conversations
bin/mempalace mine --mode convos ./conversations/

# Mine with general extraction (5-type heuristic)
bin/mempalace mine --mode convos --extract general ./conversations/

# Incremental — only re-mines changed files
bin/mempalace mine ./my-project
```

## MCP server

go-palace exposes 19 tools via the Model Context Protocol. Configure in Claude Desktop:

```json
{
  "mcpServers": {
    "mempalace": {
      "command": "/path/to/bin/mempalace",
      "args": ["mcp"]
    }
  }
}
```

### Available MCP tools

| Tool | Description |
|------|-------------|
| `mempalace_status` | Palace stats |
| `mempalace_search` | Semantic search |
| `mempalace_add_drawer` | Store a memory |
| `mempalace_delete_drawer` | Remove a memory |
| `mempalace_check_duplicate` | Check for duplicates before adding |
| `mempalace_list_wings` | List all wings |
| `mempalace_list_rooms` | List all rooms |
| `mempalace_get_taxonomy` | Get wing/room taxonomy |
| `mempalace_get_aaak_spec` | Get AAAK dialect specification |
| `mempalace_traverse` | Graph traversal from a drawer |
| `mempalace_find_tunnels` | Find cross-wing connections |
| `mempalace_graph_stats` | Palace graph statistics |
| `mempalace_kg_query` | Query knowledge graph |
| `mempalace_kg_add` | Add knowledge triple |
| `mempalace_kg_invalidate` | Invalidate a triple |
| `mempalace_kg_timeline` | Timeline of knowledge changes |
| `mempalace_kg_stats` | Knowledge graph statistics |
| `mempalace_diary_write` | Write a diary entry |
| `mempalace_diary_read` | Read diary entries |

## Configuration

### Precedence

Environment variables > `~/.mempalace/config.json` > compiled defaults.

### Environment variables

| Variable | Description |
|----------|-------------|
| `MEMPALACE_PALACE_PATH` | Override palace database location |
| `MEMPAL_PALACE_PATH` | Alias for the above |

### Config file

`~/.mempalace/config.json`:

```json
{
  "palace_path": "/custom/path/to/palace.db",
  "collection_name": "mempalace_drawers",
  "topic_wings": ["emotions", "consciousness", "memory", "technical", "identity", "family", "creative"],
  "hall_keywords": { "technical": ["code", "python", "api", "database"] }
}
```

### Palace path

Default: `~/.mempalace/palace`. Override with `--palace` flag, env var, or config file.

## Embeddings

go-palace uses [hugot](https://github.com/knights-analytics/hugot) for offline ONNX-based embeddings. No API calls, no network required after model download.

### Model setup

On first run, hugot downloads the embedding model automatically. To pre-download or use a custom model path:

```bash
bin/mempalace mine --model /path/to/model ./my-project
```

For testing, a `FakeEmbedder` is available that produces deterministic vectors without loading a model.

## Key types

| Type | Package | Description |
|------|---------|-------------|
| `Palace` | `pkg/palace` | sqlite-vec backed vector store for drawers |
| `Drawer` | `pkg/palace` | A memory unit: document + metadata (wing, room, source) |
| `Embedder` | `pkg/embed` | Interface for text-to-vector conversion |
| `Stack` | `pkg/layers` | 4-layer memory stack (L0 identity through L3 deep search) |
| `KG` | `pkg/kg` | Knowledge graph with subject-predicate-object triples |
| `Config` | `pkg/config` | Runtime configuration |
| `Searcher` | `pkg/searcher` | Semantic search over palace drawers |

## Testing

```bash
make test           # Unit tests (~643 tests)
make test/race      # Tests with race detector
make test/suite     # Behavioral equivalence suite
make test/embed     # Hugot integration tests (requires model)
make test/semantic  # Semantic search quality tests (requires model)
make audit          # Full pipeline: tidy + vet + lint + test + build
```

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE)

## Credits

go-palace is a Go port of [Python MemPalace](https://github.com/) — the original local-first memory palace for AI agents. The behavioral contracts, memory stack architecture, and AAAK dialect encoding are preserved from the Python implementation.
