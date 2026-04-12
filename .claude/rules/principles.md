---
description: Core engineering principles for all code
globs: *
---

# Principles

## KISS — Keep It Simple

- Prefira a solucao mais simples que resolve o problema.
- Nao adicione abstrações, helpers ou configurabilidade para cenarios hipoteticos.
- Tres linhas duplicadas sao melhores que uma abstracao prematura.

## Fail-Fast

- Valide inputs nas fronteiras do sistema (CLI flags, arquivos lidos, subprocess IO).
- Retorne erros imediatamente — nao acumule erros silenciosamente.
- Panics apenas para erros de programacao (invariants violados). Erros de runtime sao retornados.

## Density Over Coverage

- Testes devem cobrir comportamento, nao linhas de codigo.
- Um teste de integracao que valida o fluxo completo vale mais que 10 unit tests de getters.
- Table-driven tests para variações de input.

## Single Responsibility

- Cada pacote Go tem um dominio claro.
- Se um arquivo ultrapassa 300 linhas, provavelmente precisa ser dividido.
- CLI dispatcher delega para pacotes de dominio. Pacotes de dominio delegam para stores/embedders.

## Explicit Over Implicit

- Dependency injection via struct, nao variaveis globais.
- Interfaces pequenas e explicitas (ex: `Embedder`, `RoomRouter`).
- Configuracoes via environment variables ou arquivos JSON/YAML, nao valores hardcoded.
