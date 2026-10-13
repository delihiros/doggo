# Makefile for Doggo project

# Variables
BIN_DIR := dist/bin
DOGGO_BINARY := $(BIN_DIR)/doggo
INTEROP_GEN_BINARY := $(BIN_DIR)/interop_gen
VSCODE_EXT_DIR := editors/vscode
GO_TOOLCHAIN ?= go1.26.9
GO ?= go
GO_CMD := GOTOOLCHAIN=$(GO_TOOLCHAIN) $(GO)
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
DOGGO_LDFLAGS ?= -X main.version=$(VERSION)
BENCH ?= .
BENCHTIME ?=
BENCH_ARGS := -bench='$(BENCH)' -benchmem -run=^$$ $(if $(BENCHTIME),-benchtime=$(BENCHTIME),)
RELEASE_KEY_DIR ?= dist/keys
RELEASE_OUT_DIR ?= dist/release

# Default target
.DEFAULT_GOAL := help

# Create the build output directory if it doesn't exist
$(BIN_DIR):
	@mkdir -p $(BIN_DIR)

# Help target
.PHONY: help
help: ## Show this help message
	@echo "Available targets:"
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2}'

# Test targets
.PHONY: test
test: ## Run all Go tests
	@$(GO_CMD) test ./...

.PHONY: test-verbose
test-verbose: ## Run all Go tests with verbose output
	@$(GO_CMD) test -v ./...

.PHONY: test-coverage
test-coverage: ## Run tests and generate coverage report
	@$(GO_CMD) test -coverprofile=coverage.out ./...
	@$(GO_CMD) tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report generated: coverage.html"

# Benchmark targets
.PHONY: benchmark
benchmark: ## Run Go benchmark tests (VM + parser)
	@echo "Running Go benchmarks..."
	@echo ""
	@echo "=== VM Benchmarks ==="
	@$(GO_CMD) test $(BENCH_ARGS) ./internal/vm/
	@echo ""
	@echo "=== Parser Benchmarks ==="
	@$(GO_CMD) test $(BENCH_ARGS) ./internal/parser/

.PHONY: benchmark-vm
benchmark-vm: ## Run VM benchmarks only
	@echo "Running VM benchmarks..."
	@$(GO_CMD) test $(BENCH_ARGS) ./internal/vm/

.PHONY: benchmark-parser
benchmark-parser: ## Run parser benchmarks only
	@echo "Running parser benchmarks..."
	@$(GO_CMD) test $(BENCH_ARGS) ./internal/parser/

.PHONY: benchmark-profile-vm
benchmark-profile-vm: ## Run VM benchmarks with CPU profiling
	@echo "Running VM benchmarks with CPU profiling..."
	@$(GO_CMD) test $(BENCH_ARGS) -cpuprofile=cpu.prof ./internal/vm/
	@echo ""
	@echo "Profile saved to cpu.prof"
	@echo "View with: go tool pprof -top cpu.prof"
	@echo "Interactive: go tool pprof cpu.prof"

.PHONY: benchmark-profile-parser
benchmark-profile-parser: ## Run parser benchmarks with CPU profiling
	@echo "Running parser benchmarks with CPU profiling..."
	@$(GO_CMD) test $(BENCH_ARGS) -cpuprofile=cpu.prof ./internal/parser/
	@echo ""
	@echo "Profile saved to cpu.prof"
	@echo "View with: go tool pprof -top cpu.prof"
	@echo "Interactive: go tool pprof cpu.prof"

.PHONY: bench-profile
bench-profile: build-doggo ## Run benchmark with CPU profiling
	@echo "Running with CPU profiling..."
	@$(DOGGO_BINARY) -cpu-profile cpu.prof bench run loop-fusion/map-filter-reduce-100k --warmup 0 --iterations 3 --quiet
	@echo ""
	@echo "Profile saved to cpu.prof. View with: go tool pprof cpu.prof"

# Build targets
.PHONY: build
build: generate build-doggo build-interop-gen ## Build the Doggo CLI and interop generator

.PHONY: build-doggo
build-doggo: licenses $(BIN_DIR) ## Build unified doggo CLI with current notices
	@echo "Building doggo CLI..."
	@$(GO_CMD) build -trimpath -ldflags "$(DOGGO_LDFLAGS)" -o $(DOGGO_BINARY) ./cmd/doggo

.PHONY: build-interop-gen
build-interop-gen: $(BIN_DIR) ## Build interop generator
	@echo "Building interop generator..."
	@$(GO_CMD) build -o $(INTEROP_GEN_BINARY) ./cmd/interop_gen

# Run targets
.PHONY: run
run: build-doggo ## Run REPL using doggo command
	@$(DOGGO_BINARY)

.PHONY: run-repl
run-repl: build-doggo ## Run REPL using doggo command
	@$(DOGGO_BINARY) repl

# Code generation
.PHONY: generate
generate: ## Run code generation (interop + reify)
	@echo "Generating interop code..."
	@$(GO_CMD) generate ./internal/interop
	@echo "Generating reify code..."
	@$(GO_CMD) generate ./internal/reify

.PHONY: generate-reify
generate-reify: ## Generate reify code only
	@echo "Generating reify code..."
	@$(GO_CMD) generate ./internal/reify

# Module/dependency management targets
.PHONY: mod-init
mod-init: build-doggo ## Initialize doggo.mod in current directory
	@$(DOGGO_BINARY) mod init

.PHONY: mod-sync
mod-sync: build-doggo ## Materialize deps, generate interop/reify, and update lock
	@$(DOGGO_BINARY) mod sync

.PHONY: deps-list
deps-list: build-doggo ## List dependency sources from doggo.mod
	@$(DOGGO_BINARY) deps list

.PHONY: deps-add
deps-add: build-doggo ## Add dependency source (use DEP=<source>)
	@if [ -z "$(DEP)" ]; then \
		echo "Error: DEP variable is required. Usage: make deps-add DEP=<source>"; \
		exit 1; \
	fi
	@$(DOGGO_BINARY) deps add $(DEP)

.PHONY: deps-remove
deps-remove: build-doggo ## Remove dependency source (use DEP=<source>)
	@if [ -z "$(DEP)" ]; then \
		echo "Error: DEP variable is required. Usage: make deps-remove DEP=<source>"; \
		exit 1; \
	fi
	@$(DOGGO_BINARY) deps remove $(DEP)

.PHONY: deps-sync
deps-sync: build-doggo ## Add dependency source and sync (use DEP=<source>)
	@if [ -z "$(DEP)" ]; then \
		echo "Error: DEP variable is required. Usage: make deps-sync DEP=<source>"; \
		exit 1; \
	fi
	@$(DOGGO_BINARY) deps add $(DEP)
	@$(DOGGO_BINARY) mod sync

# Release supply-chain targets
.PHONY: release-keygen
release-keygen: build-doggo ## Generate ed25519 key pair for release bundles
	@$(DOGGO_BINARY) release keygen --key-dir $(RELEASE_KEY_DIR)

.PHONY: release-bundle
release-bundle: build-doggo vscode-package ## Generate checksums + SBOM + required signature for CLI/VSIX
	@$(DOGGO_BINARY) build --sync
	@$(DOGGO_BINARY) release bundle --out-dir $(RELEASE_OUT_DIR) --artifacts $(VSCODE_EXT_DIR)/dist/doggo-vscode.vsix,LICENSE,THIRD_PARTY_NOTICES

.PHONY: licenses
licenses: ## Generate third-party notices for all release targets
	@$(GO_CMD) run ./cmd/license-notices

.PHONY: licenses-check
licenses-check: ## Verify tracked third-party notices
	@$(GO_CMD) run ./cmd/license-notices -check

.PHONY: release-verify
release-verify: build-doggo ## Verify checksums/signature and require SBOM files
	@$(DOGGO_BINARY) release verify --out-dir $(RELEASE_OUT_DIR)

.PHONY: release-gate
release-gate: build-doggo ## Run dependency audit gate and release integrity-file checks
	@$(DOGGO_BINARY) release gate --out-dir $(RELEASE_OUT_DIR)

# Cleanup targets
.PHONY: clean
clean: ## Remove build binaries and Go coverage outputs
	@echo "Cleaning build artifacts..."
	@rm -rf $(BIN_DIR)
	@rm -f coverage.out coverage.html

.PHONY: clean-generated
clean-generated: clean ## Also remove generated interop and reify Go files
	@echo "Cleaning generated files..."
	@rm -f internal/interop/generated.go
	@rm -f internal/reify/generated_reify.go

# VS Code extension
.PHONY: vscode-install
vscode-install: ## Install dependencies for VS Code extension
	@cd $(VSCODE_EXT_DIR) && npm install

.PHONY: vscode-build
vscode-build: vscode-install ## Compile VS Code extension
	@cd $(VSCODE_EXT_DIR) && npm run compile

.PHONY: vscode-test
vscode-test: vscode-install ## Run VS Code extension unit tests
	@cd $(VSCODE_EXT_DIR) && npm run test

.PHONY: vscode-package
vscode-package: vscode-install ## Build VSIX package for VS Code extension
	@cd $(VSCODE_EXT_DIR) && npm run package

.PHONY: vscode-verify
vscode-verify: vscode-install ## Run VS Code extension release quality gate
	@cd $(VSCODE_EXT_DIR) && npm run verify
