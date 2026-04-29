.PHONY: help build test test-race test-bench test-e2e lint vet fmt clean run install snapshot tag-version regress

BIN_NAME := inkwell
BIN_DIR  := bin
PKG      := github.com/eugenelim/inkwell
VERSION  := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT   := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
DATE     := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS  := -s -w \
            -X main.version=$(VERSION) \
            -X main.commit=$(COMMIT) \
            -X main.date=$(DATE)

help:
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

build: ## Build the binary into ./bin/inkwell
	@mkdir -p $(BIN_DIR)
	go build -trimpath -ldflags='$(LDFLAGS)' -o $(BIN_DIR)/$(BIN_NAME) ./cmd/inkwell

install: ## go install into $GOBIN
	go install -trimpath -ldflags='$(LDFLAGS)' ./cmd/inkwell

run: build ## Build and exec
	./$(BIN_DIR)/$(BIN_NAME) $(ARGS)

test: ## Unit tests with race detector
	go test -race ./...

test-short: ## Quick test pass (skips heavy budget benches)
	go test -race -short ./...

test-e2e: ## TUI end-to-end tests
	go test -tags=e2e ./...

test-bench: ## Benchmarks (no race; race inflates per-op time)
	go test -bench=. -benchmem -run='^$$' ./...

test-budgets: ## Spec §7 budget gate (skipped under -race; this is the gating run)
	go test -timeout 600s -run TestBudgetsHonoured -v ./internal/store/...

test-all: test test-e2e ## Race + e2e

regress: ## Full regression suite (CLAUDE.md §5.8). Run before tagging.
	@./scripts/regress.sh

vet:
	go vet ./...

fmt: ## Format with gofmt -s
	gofmt -s -w .

lint: ## staticcheck (install: go install honnef.co/go/tools/cmd/staticcheck@latest)
	@which staticcheck >/dev/null 2>&1 || { echo "install staticcheck: go install honnef.co/go/tools/cmd/staticcheck@latest"; exit 1; }
	staticcheck ./...

snapshot: ## Local goreleaser snapshot build (no publish)
	@which goreleaser >/dev/null 2>&1 || { echo "install goreleaser: brew install goreleaser/tap/goreleaser"; exit 1; }
	goreleaser release --snapshot --clean

clean:
	rm -rf $(BIN_DIR) dist

tag-version: ## Print the SemVer tag this checkout would produce
	@echo $(VERSION)
