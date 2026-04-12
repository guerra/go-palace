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
.PHONY: test/suite
test/suite:
	@echo "TODO: wire behavioral suite (see mempalace-go-test-suite-result.md)"
	@exit 1

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

## audit: full pipeline (tidy + vet + lint + test + build)
.PHONY: audit
audit: tidy vet lint test build
	@echo "audit: PASS"
