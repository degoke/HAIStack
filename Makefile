.PHONY: help fmt format fmt-check format-check vet lint test test-race build tidy clean ci all

GO ?= go
GOPATH_BIN := $(shell $(GO) env GOPATH)/bin
ifneq ($(wildcard $(GOPATH_BIN)/golangci-lint),)
GOLANGCI_LINT ?= $(GOPATH_BIN)/golangci-lint
else
GOLANGCI_LINT ?= golangci-lint
endif

help: ## Show available targets
	@awk 'BEGIN {FS = ":.*##"; printf "Usage: make <target>\n\nTargets:\n"} /^[a-zA-Z0-9_-]+:.*##/ {printf "  %-18s %s\n", $$1, $$2}' $(MAKEFILE_LIST)

fmt format: ## Format Go source files
	$(GO) fmt ./...

fmt-check format-check: ## Verify Go files are formatted
	@files=$$($(GO)fmt -l .); \
	if [ -n "$$files" ]; then \
		echo "Unformatted files (run 'make fmt'):"; \
		echo "$$files"; \
		exit 1; \
	fi

vet: ## Run go vet
	$(GO) vet ./...

install-lint: ## Install golangci-lint (matches CI version)
	$(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2

lint: ## Run golangci-lint
	@test -x "$(GOLANGCI_LINT)" || { echo "golangci-lint not found. Run 'make install-lint' or add $(GOPATH_BIN) to PATH."; exit 1; }
	$(GOLANGCI_LINT) run ./...

test: ## Run unit and integration tests
	$(GO) test ./...

test-race: ## Run tests with the race detector
	$(GO) test -race -count=1 ./...

build: ## Build all packages
	$(GO) build ./...
	$(GO) build -o bin/haistack ./cmd/haistack

tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy

generate-r4-bundle: ## Regenerate embedded HL7 FHIR R4 base catalog
	python3 scripts/generate-r4-bundle.py

clean: ## Remove build artifacts and test binaries
	$(GO) clean -testcache
	rm -f coverage.out coverage.html

ci: fmt-check vet lint test-race build ## Run all CI checks locally

all: fmt vet lint test build ## Run format, vet, lint, test, and build
