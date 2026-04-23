# Makefile

.DEFAULT_GOAL := help
MAKEFLAGS += --no-print-directory

BINARY := gh-pulse

.PHONY: help test test-race vet fmt fix check build

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"} /^[a-zA-Z0-9_.-]+:.*##/ {printf "  %-12s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

test: ## Run tests
	go test ./...

test-race: ## Run tests with race detector
	go test -race ./...

vet: ## Run static analysis
	go vet ./...

fmt: ## Format code
	go fmt ./...

fix: ## Apply automated Go source rewrites
	go fix ./...

check: ## Run full validation suite except race tests
	$(MAKE) fix
	$(MAKE) fmt
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) build

build: ## Build binary
	go build -o $(BINARY) ./...
