---
description: Go concurrency patterns — context scopes, errgroup, worker pools, graceful shutdown
globs: "**/*.go"
---

# Go Concurrency

## Dois Escopos de Context

- **App-scoped**: `signal.NotifyContext` em `main.go` — para CLI commands de longa duracao (mine, MCP server).
- **Operation-scoped**: derivado do app context, passado para pacotes de dominio (palace, miner, searcher).
- Background goroutines que sobrevivem ao command usam app context.
- Nunca armazenar context em structs. Passar como primeiro parametro.

## errgroup para Orquestracao

```go
g, gCtx := errgroup.WithContext(ctx)

g.Go(func() error { return miner.Run(gCtx, opts) })
g.Go(func() error {
    <-gCtx.Done()
    return palace.Close()
})

if err := g.Wait(); err != nil {
    slog.Error("mine failed", "error", err)
    os.Exit(1)
}
```

- Shutdown de recursos (db, embedder subprocess) deve rodar apos cancelamento, com context novo se necessario.

## Worker Pool (Miner)

O miner processa N arquivos concorrentemente (default 4). Use `errgroup.SetLimit` para bounded parallelism:

```go
func (m *Miner) processFiles(ctx context.Context, files []string) error {
    g, gCtx := errgroup.WithContext(ctx)
    g.SetLimit(m.workers)

    for _, f := range files {
        f := f
        g.Go(func() error {
            return m.processFile(gCtx, f)
        })
    }
    return g.Wait()
}
```

- Erros per-file sao logados mas nao abortam o batch (match Python `miner.py` behavior).
- Context cancelamento aborta todos os workers.

## Channels vs Mutexes

- **Channels**: passar ownership de dados, distribuir work, comunicar resultados.
- **Mutexes**: proteger estado compartilhado (caches, counters, connection pool).
- "Share memory by communicating" NAO significa "sempre use channels".
- `sync.Mutex` e mais simples e rapido para guardar estado.

## Prevencao de Goroutine Leaks

- Todo `go` statement DEVE ter exit path via context ou channel.
- Testar com `-race` flag (`make test/race`).

```go
// ERRADO — goroutine vaza se ctx cancelado antes do ch receber
go func() {
    ch <- expensiveWork()
}()

// CORRETO — goroutine sai no cancelamento
go func() {
    select {
    case ch <- expensiveWork(ctx):
    case <-ctx.Done():
    }
}()
```

## Graceful Shutdown

- `signal.NotifyContext` para capturar SIGINT/SIGTERM.
- MCP server (stdin/stdout loop) encerra quando stdin fecha OU context cancela.
- Palace (sqlite connection) deve fechar com timeout via context derivado.
- Aguardar workers finalizarem antes de sair (errgroup.Wait).
