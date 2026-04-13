# Contributing to go-palace

Thank you for your interest in contributing.

## Prerequisites

- **Go 1.26+**
- **CGO enabled** — required for sqlite-vec and go-sqlite3
- **golangci-lint** — `go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest`

## Building

```bash
make build        # compiles bin/mempalace (CGO_ENABLED=1)
```

## Running tests

```bash
make test         # unit tests
make test/race    # unit tests with race detector
make test/suite   # behavioral equivalence suite
make audit        # full pipeline: tidy + vet + lint + test + build
```

For embedding integration tests (requires model download on first run):

```bash
make test/embed
make test/semantic
```

## Code style

- Run `make lint` before submitting. The project uses golangci-lint with errcheck, staticcheck, revive, goimports, and misspell.
- Follow existing patterns in the codebase. Public API lives in `pkg/`, private implementation in `internal/`.
- Tests go next to the code they test. Use table-driven tests where appropriate.

## Submitting changes

1. Fork the repository and create a feature branch.
2. Make your changes. Add tests for new functionality.
3. Run `make audit` — all checks must pass.
4. Open a pull request with a clear description of what and why.

## Reporting issues

Open a GitHub issue with:
- What you expected
- What happened instead
- Steps to reproduce
- Go version and OS
