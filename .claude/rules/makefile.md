---
description: Makefile conventions and target usage
globs: Makefile
---

# Makefile

## Targets Obrigatorios

| Target        | Descricao                                    |
| ------------- | -------------------------------------------- |
| `run`         | Roda o CLI localmente                        |
| `build`       | Compila binario para `bin/mempalace`         |
| `test`        | Roda testes                                  |
| `test/race`   | Testes com race detector                     |
| `test/cover`  | Testes com coverage report                   |
| `test/suite`  | Roda behavioral equivalence suite (Go vs Python oracle) |
| `audit`       | Pipeline completa: vet + test + build        |
| `tidy`        | `go mod tidy`                                |
| `help`        | Lista todos os targets disponiveis           |

## Convencoes

- `audit` e o gate de qualidade. Deve passar antes de qualquer commit.
- Binario sempre em `bin/` (gitignored).
- `test/suite` requer Python reference impl instalada (`pip install mempalace` ou checkout local).
- Targets com `/` indicam variantes: `test/race`, `test/cover`, `test/suite`.
- `help` e o target default (roda com `make` sem argumentos).
- Build requires CGO (sqlite-vec + go-sqlite3). `CGO_ENABLED=1` no target build.
