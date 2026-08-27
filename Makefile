# ──────────────────────────────────────────────────────────────────────
# crypto-bot — Code Quality & Build Automation
# ──────────────────────────────────────────────────────────────────────
# Usage:
#   make <target>               (Linux/macOS/CI)
# ──────────────────────────────────────────────────────────────────────

# ── Configuration ────────────────────────────────────────────────────
GO              := go
GOLANGCI_LINT   := go tool golangci-lint
MODERNIZE       := go tool modernize
GOIMPORTS       := go tool goimports
COVERAGE_DIR    := .coverage
COVERAGE_FILE   := $(COVERAGE_DIR)/coverage.out
COVERAGE_HTML   := $(COVERAGE_DIR)/coverage.html
MIN_COVERAGE    := 80
GREP_V_MOCKS    := grep -v "mocks"
EXCLUDE_EXCHANGES := (batonex|bingx|bitfinex|bitmex|coinex|cryptocom|deribit|digifinex|dydx|hashkey|htx|phemex|woox|zoomex|krakenfutures|coinw|hyperliquid|sunx)
TEST_PKGS       := $(shell $(GO) list ./internal/... ./pkg/... | $(GREP_V_MOCKS) | grep -E -v "/exchange/$(EXCLUDE_EXCHANGES)(/|$$)")

# Registry Configuration
REGISTRY        ?= ghcr.io/dangnmh
IMAGE_NAME      ?= crypto-bot
IMAGE_TAG       ?= latest
FULL_IMAGE      := $(REGISTRY)/$(IMAGE_NAME):$(IMAGE_TAG)

# Build version metadata
VERSION         ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "dev")
COMMIT          ?= $(VERSION)
BUILD_TIME      ?= $(shell git show -s --format=%cI HEAD 2>/dev/null || date -u +'%Y-%m-%dT%H:%M:%SZ' 2>/dev/null || echo "unknown")

LDFLAGS         := -w -s \
                   -X crypto-bot/pkg/version.Version=$(VERSION) \
                   -X crypto-bot/pkg/version.Commit=$(COMMIT) \
                   -X crypto-bot/pkg/version.BuildTime=$(BUILD_TIME)

# ── Modular Makefiles Inclusion ──────────────────────────────────────
-include make/local.mk
-include make/prod.mk

# ── Build & Generate ───────────────────────────────────────────────────
.PHONY: gen
gen: ## Generate mocks and other generated files
	$(GO) generate ./...

.PHONY: build
build: ## Build all binaries
	$(GO) build -ldflags="$(LDFLAGS)" ./...

.PHONY: build-funding
build-funding: ## Build Funding Bot binary
	$(GO) build -ldflags="$(LDFLAGS)" -o bin/funding-bot ./cmd/funding

.PHONY: build-penny-jumper
build-penny-jumper: ## Build Penny Jumper Bot binary
	$(GO) build -ldflags="$(LDFLAGS)" -o bin/penny-jumper-bot ./cmd/penny_jumper

.PHONY: docker-build
docker-build: ## Build and tag Docker container image locally and for registry
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) -t $(FULL_IMAGE) .

.PHONY: docker-push
docker-push: docker-build ## Build and Push Docker image to registry
	docker push $(FULL_IMAGE)

# ── Test ─────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all unit tests
	$(GO) test -count=1 $(TEST_PKGS)

.PHONY: test-verbose
test-verbose: ## Run all tests with verbose output
	$(GO) test -count=1 -v $(TEST_PKGS)

.PHONY: test-race
test-race: ## Run all tests with race detector (requires GCC/C compiler)
	CGO_ENABLED=1 $(GO) test -count=1 -race $(TEST_PKGS)

# ── Coverage ─────────────────────────────────────────────────────────
.PHONY: cover
cover: ## Run tests with coverage and display summary
	@mkdir -p $(COVERAGE_DIR)
	$(GO) test -count=1 -coverprofile=$(COVERAGE_FILE) -covermode=atomic $(TEST_PKGS)
	@echo ""
	@echo "======== COVERAGE SUMMARY ========"
	@$(GO) tool cover -func $(COVERAGE_FILE) | tail -n 1
	@echo "=================================="

.PHONY: cover-html
cover-html: cover ## Generate HTML coverage report and open in browser
	$(GO) tool cover -html=$(COVERAGE_FILE) -o $(COVERAGE_HTML)
	@echo "Report: $(COVERAGE_HTML)"
	@if command -v xdg-open >/dev/null 2>&1; then \
		xdg-open $(COVERAGE_HTML); \
	elif command -v open >/dev/null 2>&1; then \
		open $(COVERAGE_HTML); \
	else \
		echo "Could not open browser automatically."; \
	fi

.PHONY: cover-check
cover-check: cover ## Enforce minimum coverage threshold
	@PCT=$$($(GO) tool cover -func $(COVERAGE_FILE) | tail -n 1 | awk '{print $$3}' | sed 's/%//'); \
	echo "Total: $$PCT%  min: $(MIN_COVERAGE)%"; \
	awk -v pct="$$PCT" -v min="$(MIN_COVERAGE)" 'BEGIN { if (pct < min) { print "FAIL: Below threshold"; exit 1 } else { print "PASS: Coverage OK"; exit 0 } }'

# ── Lint & Format ────────────────────────────────────────────────────
.PHONY: lint
lint: mod-tidy fmt vet ## Run golangci-lint
	$(GOLANGCI_LINT) run ./...

.PHONY: lint-fix
lint-fix: mod-tidy fmt vet ## Run golangci-lint with auto-fix
	$(GOLANGCI_LINT) run --fix ./...

.PHONY: fmt
fmt: ## Format all Go files
	$(GO) fmt ./...
	$(GOIMPORTS) -w .

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...
	$(MODERNIZE) -fix ./...

# ── Modules ──────────────────────────────────────────────────────────
.PHONY: upgrade
upgrade: ## Upgrade dependencies in go.mod
	$(GO) get -u ./...

.PHONY: mod-tidy
mod-tidy: ## Tidy go.mod and go.sum
	$(GO) mod tidy
	$(GO) mod verify

# ── Quality Gate ─────────────────────────────────────────────────────
.PHONY: ci
ci: mod-tidy fmt vet lint test cover-check ## Full CI pipeline

.PHONY: pre-commit
pre-commit: fmt vet lint test ## Quick pre-commit check

# ── SonarQube ────────────────────────────────────────────────────────
SONAR_SCANNER := sonar-scanner
TEST_REPORT    := $(COVERAGE_DIR)/test-report.json
LINT_REPORT    := $(COVERAGE_DIR)/golangci-lint-report.xml

.PHONY: sonar-reports
sonar-reports: ## Generate all reports for SonarQube (coverage + test + lint)
	@mkdir -p $(COVERAGE_DIR)
	@echo "[SONAR] Generating coverage profile + test report..."
	-$(GO) test -count=1 -coverprofile=$(COVERAGE_FILE) -covermode=atomic -json $(TEST_PKGS) > $(TEST_REPORT) 2>&1
	@echo "[SONAR] Generating lint report..."
	-$(GOLANGCI_LINT) run --out-format checkstyle ./... > $(LINT_REPORT) 2>/dev/null
	@echo "[SONAR] Reports ready in $(COVERAGE_DIR)/"
	@echo "   $(COVERAGE_FILE)"
	@echo "   $(TEST_REPORT)"
	@echo "   $(LINT_REPORT)"

.PHONY: sonar
sonar: sonar-reports ## Run SonarQube analysis (requires sonar-scanner + SONAR_TOKEN)
	@echo ""
	@echo "[SONAR] Running SonarQube scanner..."
	$(SONAR_SCANNER)
	@echo "[SONAR] Analysis complete"

.PHONY: sonar-check
sonar-check: ## Verify SonarQube prerequisites
	@echo "Checking SonarQube prerequisites..."
	@if command -v $(SONAR_SCANNER) >/dev/null 2>&1; then echo "  [OK] sonar-scanner found"; else echo "  [MISSING] sonar-scanner not found"; fi
	@if [ -n "$$SONAR_HOST_URL" ]; then echo "  [OK] SONAR_HOST_URL = $$SONAR_HOST_URL"; else echo "  [WARN] SONAR_HOST_URL not set (defaults to http://localhost:9000)"; fi
	@if [ -n "$$SONAR_TOKEN" ]; then echo "  [OK] SONAR_TOKEN is set"; else echo "  [MISSING] SONAR_TOKEN not set"; fi

# ── Clean ────────────────────────────────────────────────────────────
.PHONY: clean
clean: ## Remove build artifacts and coverage
	rm -rf bin $(COVERAGE_DIR)
	$(GO) clean -cache -testcache

# ── Help ─────────────────────────────────────────────────────────────
.PHONY: help
help: ## Show available targets
	@echo ""
	@echo "  crypto-bot Makefile targets"
	@echo ""
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_\/-]+:.*?## / {printf "  %-22s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""

.DEFAULT_GOAL := help
