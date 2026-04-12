---
description: Go core idioms — project structure, interfaces, DI, error handling, imports, logging, config
globs: "**/*.go"
---

# Go Patterns

## Project Structure

- Todo codigo privado fica em `internal/`.
- Um pacote por dominio: `config/`, `palace/`, `miner/`, `convominer/`, `normalize/`, `extractor/`, `searcher/`, `layers/`, `dialect/`, `graph/`, `kg/`, `entity/`, `room/`, `spellcheck/`, `splitter/`, `hooks/`, `embed/`.
- CLI entry em `cmd/mempalace/main.go`. MCP server em `mcp/` (publico).
- Interfaces definidas no pacote que as consome, nao no que as implementa.

## Interfaces

- Defina interfaces no pacote que as consome, nao no que as implementa.
- Interfaces pequenas (1-3 metodos). Nao crie interfaces "god" com tudo.

```go
// Em palace/palace.go
type Embedder interface {
    Embed(texts []string) ([][]float32, error)
    Dimension() int
}

// Em miner/miner.go
type RoomRouter interface {
    DetectRoom(filepath, content string, rooms []Room) string
}
```

## Dependency Injection

- Via struct constructor. Sem frameworks de DI.
- Services recebem stores e embedders. CLI handlers recebem services.

```go
func Open(path string, embedder Embedder) (*Palace, error) {
    // ...
    return &Palace{db: db, embedder: embedder}, nil
}
```

## Error Handling

- Sempre retorne errors com contexto: `fmt.Errorf("upsert drawer: %w", err)`
- Nunca engula erros. Se nao vai tratar, propague.
- Use `errors.Is()` e `errors.As()` para checagem de tipo.
- Erros de dominio como tipos: `var ErrNoPalace = errors.New("palace not found")`, `var ErrNotFound = errors.New("not found")`.

## Imports

Ordem fixa com blank lines separando grupos:

```go
import (
    "context"
    "fmt"

    "github.com/spf13/cobra"
    _ "github.com/mattn/go-sqlite3"

    "mempalace/internal/palace"
)
```

## Logging

- `log/slog` com structured fields.
- Sempre inclua `palace_path`, `wing`, `room`, `source_file` quando disponiveis no contexto.
- Nao use `log.Fatal` exceto em `main.go`.
- Log config carregada no startup (excluir secrets).

## Configuration

### Struct Config com Helpers Manuais

Em `pkg/config/config.go`. Sem `viper`:

```go
func Load(configDir string) (*Config, error) {
    cfg := &Config{
        PalacePath:     envOrDefault("MEMPALACE_PATH", defaultPalacePath(configDir)),
        CollectionName: envOrDefault("MEMPALACE_COLLECTION", "mempalace_drawers"),
    }

    if err := cfg.loadFromFile(filepath.Join(configDir, "config.json")); err != nil {
        slog.Warn("config.json parse failed, using defaults", "error", err)
    }

    return cfg, nil
}
```

### Env Helpers (~30 linhas)

```go
func envOrDefault(key, fallback string) string {
    if v := os.Getenv(key); v != "" {
        return v
    }
    return fallback
}

func envRequired(key string, errs *[]string) string {
    v := os.Getenv(key)
    if v == "" {
        *errs = append(*errs, key)
    }
    return v
}

func envDurationOrDefault(key string, fallback time.Duration) time.Duration {
    v := os.Getenv(key)
    if v == "" {
        return fallback
    }
    d, err := time.ParseDuration(v)
    if err != nil {
        slog.Warn("invalid duration, using default", "key", key, "value", v, "default", fallback)
        return fallback
    }
    return d
}
```

- Prioridade estrita: env vars > arquivo JSON > defaults hardcoded (match Python `config.py`).
- JSON parse failures fall back silently to defaults.
- Config e carregada uma vez no startup. Sem reload em runtime.
- Uma vez construido, `*Config` e valido — confiar internamente.

## SQL Safety (SQLite)

- **NUNCA** usar `fmt.Sprintf` para construir queries. Todos os valores via `?`.
- Para nomes dinamicos de coluna/tabela: allowlist explicito.

```go
// CORRETO — parametrizado
db.QueryRowContext(ctx, "SELECT document FROM drawers WHERE id = ? AND wing = ?", id, wing)

// PROIBIDO — interpolacao de string
query := fmt.Sprintf("SELECT * FROM drawers WHERE wing = '%s'", wing)
```

## Input Validation

Validar nas fronteiras: CLI flags, arquivos lidos, input de subprocess. Collect-all-errors quando fizer sentido:

```go
func (o MineOptions) Valid() []string {
    var problems []string
    if o.ProjectDir == "" {
        problems = append(problems, "project dir is required")
    }
    if o.Limit < 0 {
        problems = append(problems, "limit must be >= 0")
    }
    return problems
}
```

- Validar no handler/CLI, confiar no service. Uma vez construido, o tipo e valido.
- UUID via `google/uuid.Parse()`. Paths via `filepath.Clean()` + `filepath.IsAbs()` quando aplicavel.
- String fields: enforce max length, trim whitespace, reject control characters.
- Allowlists, nao blocklists.
