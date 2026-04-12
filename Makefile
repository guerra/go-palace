.DEFAULT_GOAL := help

BINARY := bin/mempalace
PKG := ./...

export CGO_ENABLED := 1

## help: list targets
.PHONY: help
help:
	@awk 'BEGIN{FS=":.*?## "} /^##/ {sub(/^## /,""); print}' $(MAKEFILE_LIST)

## run: run CLI (pass ARGS="status" etc)
.PHONY: run
run:
	go run ./cmd/mempalace $(ARGS)

## build: compile binary to bin/mempalace
.PHONY: build
build:
	go build -o $(BINARY) ./cmd/mempalace

## test: run unit tests
.PHONY: test
test:
	go test $(PKG)

## test/race: run tests with race detector
.PHONY: test/race
test/race:
	go test -race $(PKG)

## test/cover: run tests with coverage report
.PHONY: test/cover
test/cover:
	go test -coverprofile=coverage.out $(PKG)
	go tool cover -func=coverage.out

## test/suite: run behavioral equivalence suite vs Python oracle
##   Defaults to MEMPALACE_IMPL=go so this target passes without uv/python.
##   Set MEMPALACE_IMPL=both to additionally drive the Python oracle.
.PHONY: test/suite
test/suite: build
	MEMPALACE_GO_BIN=$(CURDIR)/$(BINARY) go test -tags=testsuite ./testsuite/...

## lint: run golangci-lint
.PHONY: lint
lint:
	golangci-lint run $(PKG)

## vet: run go vet
.PHONY: vet
vet:
	go vet $(PKG)

## tidy: go mod tidy
.PHONY: tidy
tidy:
	go mod tidy

## test/embed: run hugot integration tests (requires model download)
.PHONY: test/embed
test/embed:
	go test -count=1 -tags=integration -run TestHugotEmbedder ./internal/embed/

## test/semantic: run semantic search quality tests (requires model download)
.PHONY: test/semantic
test/semantic:
	go test -count=1 -tags=integration -run TestSemantic ./testsuite/

## audit: full pipeline (tidy + vet + lint + test + build)
.PHONY: audit
audit: tidy vet lint test build
	@echo "audit: PASS"
