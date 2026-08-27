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
FUNDING_SYS     := ./configs/funding/local/system.jsonc
FUNDING_EXCH    := ./configs/funding/local/exchange.jsonc
FUNDING_BOT     := ./configs/funding/local/funding.jsonc
FUNDING_BLK     := ./configs/funding/local/blacklist.jsonc
FUNDING_REV     := ./configs/funding/local/reversion.jsonc
FUNDING_OBF     := ./configs/funding/local/obfuscator.jsonc
FUNDING_DIL     := ./configs/funding/local/dilution.jsonc

PENNY_JUMPER_SYS  := ./configs/penny_jumper/local/system.jsonc
PENNY_JUMPER_EXCH := ./configs/penny_jumper/local/exchange.jsonc
PENNY_JUMPER_BOT  := ./configs/penny_jumper/local/penny_jumper.jsonc
PENNY_JUMPER_BLK  := ./configs/penny_jumper/local/blacklist.jsonc

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

# ── Build & Generate ───────────────────────────────────────────────────
.PHONY: gen
gen: ## Generate mocks and other generated files
	$(GO) generate ./...

.PHONY: build
build: ## Build all binaries
	$(GO) build -ldflags="$(LDFLAGS)" ./...

.PHONY: build-funding
build-funding: ## Build the funding bot
	$(GO) build -ldflags="$(LDFLAGS)" -o bin/funding-bot ./cmd/funding

.PHONY: build-penny-jumper
build-penny-jumper: ## Build the penny jumper bot
	$(GO) build -ldflags="$(LDFLAGS)" -o bin/penny-jumper-bot ./cmd/penny_jumper

.PHONY: docker-build
docker-build: ## Build and tag the Docker container image locally and for registry
	docker build \
		--build-arg VERSION=$(VERSION) \
		--build-arg COMMIT=$(COMMIT) \
		--build-arg BUILD_TIME=$(BUILD_TIME) \
		-t $(IMAGE_NAME):$(IMAGE_TAG) -t $(FULL_IMAGE) .

.PHONY: docker-push
docker-push: docker-build ## Build and Push the Docker image to registry
	docker push $(FULL_IMAGE)

# ── Run ───────────────────────────────────────────────────────────────
.PHONY: run/funding
run/funding: ## Run the funding bot
	$(GO) run ./cmd/funding -sys $(FUNDING_SYS) -exch $(FUNDING_EXCH) -bot $(FUNDING_BOT) -blacklist $(FUNDING_BLK) -reversion $(FUNDING_REV) -obfuscator $(FUNDING_OBF) -dilution $(FUNDING_DIL)

.PHONY: run/penny-jumper
run/penny-jumper: ## Run the penny jumper bot
	$(GO) run ./cmd/penny_jumper -sys $(PENNY_JUMPER_SYS) -exch $(PENNY_JUMPER_EXCH) -bot $(PENNY_JUMPER_BOT) -blacklist $(PENNY_JUMPER_BLK)

.PHONY: run/penny_jumper
run/penny_jumper: run/penny-jumper ## Alias for run/penny-jumper

.PHONY: scan/funding
scan/funding: ## Scan funding rates across supported futures exchanges. Usage: make scan/funding [exchanges=binance,bybit] [minFundingRate=0.1] [minVol=1000000]
	$(GO) run ./tools/scanner $(if $(exchanges),-exchanges $(exchanges),-exchanges toobit,mexc) $(if $(minFundingRate),-minFundingRate $(minFundingRate),) $(if $(minVol),-minVol $(minVol),)

# ── Test ─────────────────────────────────────────────────────────────
.PHONY: test
test: ## Run all tests
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
# 	$(GO) run golang.org/x/vuln/cmd/govulncheck@latest -show verbose ./...

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
# Prerequisites:
#   1. Install sonar-scanner:  https://docs.sonarsource.com/sonarqube-server/latest/analyzing-source-code/scanners/sonarscanner/
#   2. Set env vars:           SONAR_HOST_URL, SONAR_TOKEN
#   3. Run:                    make sonar

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

# ── Local Development (Docker Compose) ───────────────────────────────
.PHONY: dev-up
dev-up: ## Start the local development environment using Docker Compose (with rebuild)
	docker compose up -d --build

.PHONY: dev-infra
dev-infra: ## Start only the local infrastructure (PostgreSQL, Grafana & AI Proxy, excluding the bot)
	docker compose up -d postgres grafana proxy

.PHONY: proxy
proxy: ## Start the dedicated AI proxy service (CLIProxyAPI)
	docker compose up -d proxy

.PHONY: proxy-logs
proxy-logs: ## Watch logs from the AI proxy container
	docker compose logs -f proxy

.PHONY: proxy-down
proxy-down: ## Stop the AI proxy container
	docker compose stop proxy

.PHONY: dev-down
dev-down: ## Stop the local development environment
	docker compose down

.PHONY: dev-logs
dev-logs: ## Watch logs from the local development containers
	docker compose logs -f

.PHONY: dev-ps
dev-ps: ## List running local development containers
	docker compose ps


# ── Terraform ────────────────────────────────────────────────────────
.PHONY: tf-init
tf-init: ## Initialize Terraform configurations
	terraform -chdir=deploy/terraform init

.PHONY: tf-apply
tf-apply: ## Apply Terraform configurations
	terraform -chdir=deploy/terraform apply

.PHONY: tf-destroy
tf-destroy: ## Destroy Terraform configurations (keeping PVC data due to resource policy)
	terraform -chdir=deploy/terraform destroy

.PHONY: tf-apply-bots
tf-apply-bots: ## Apply changes only to all trading bot deployments
	terraform -chdir=deploy/terraform apply \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-destroy-bots
tf-destroy-bots: ## Destroy only trading bot deployments, preserving infrastructure & monitoring
	terraform -chdir=deploy/terraform destroy \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-apply-proxy
tf-apply-proxy: ## Apply changes only to the AI proxy deployment
	terraform -chdir=deploy/terraform apply \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy

.PHONY: tf-destroy-proxy
tf-destroy-proxy: ## Destroy only the AI proxy deployment
	terraform -chdir=deploy/terraform destroy \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy

.PHONY: tf-apply-infra
tf-apply-infra: ## Apply only infrastructure configurations (DB, Vault, Loki Stack, Prometheus, and ConfigMaps)
	terraform -chdir=deploy/terraform apply \
		-target=helm_release.postgresql \
		-target=helm_release.vault \
		-target=helm_release.vault_secrets_operator \
		-target=helm_release.loki_stack \
		-target=helm_release.prometheus \
		-target=kubernetes_service_account_v1.crypto_bot \
		-target=kubernetes_config_map_v1.grafana_datasource_loki \
		-target=kubernetes_config_map_v1.grafana_dashboard_pnl \
		-target=kubernetes_config_map_v1.grafana_dashboard_funding_stats \
		-target=kubernetes_config_map_v1.grafana_dashboard_trades \
		-target=kubernetes_config_map_v1.grafana_datasource_postgres \
		-target=kubernetes_config_map_v1.grafana_datasource_prometheus \
		-target=kubernetes_secret_v1.registry_pull_secret

.PHONY: tf-destroy-infra
tf-destroy-infra: ## Destroy only infrastructure configurations (DB, Vault, Loki Stack, Prometheus, and ConfigMaps)
	terraform -chdir=deploy/terraform destroy \
		-target=helm_release.postgresql \
		-target=helm_release.vault \
		-target=helm_release.vault_secrets_operator \
		-target=helm_release.loki_stack \
		-target=helm_release.prometheus \
		-target=kubernetes_service_account_v1.crypto_bot \
		-target=kubernetes_config_map_v1.grafana_datasource_loki \
		-target=kubernetes_config_map_v1.grafana_dashboard_pnl \
		-target=kubernetes_config_map_v1.grafana_dashboard_funding_stats \
		-target=kubernetes_config_map_v1.grafana_dashboard_trades \
		-target=kubernetes_config_map_v1.grafana_datasource_postgres \
		-target=kubernetes_config_map_v1.grafana_datasource_prometheus \
		-target=kubernetes_secret_v1.registry_pull_secret

.PHONY: destroy-bot
destroy-bot: ## Destroy trading bot deployments (Usage: make destroy-bot [bot=NAME] [fast=true])
	@chmod +x scripts/destroy-bot.sh
	./scripts/destroy-bot.sh $(bot) $(if $(filter true 1 yes,$(fast)),--fast,)

.PHONY: destroy-all
destroy-all: ## Destroy everything, including Go bot, Loki/Grafana, and all historical data
	@chmod +x scripts/destroy-all.sh
	./scripts/destroy-all.sh

.PHONY: destroy-pgsql
destroy-pgsql: ## Destroy all PostgreSQL deployment resources, configurations, and data volume
	@chmod +x scripts/destroy-pgsql.sh
	./scripts/destroy-pgsql.sh


.PHONY: apply-fd-configs
apply-fd-configs: ## Hot-reload Funding Bot configurations to running K8s cluster
	kubectl create configmap funding-configs \
		--from-file=configs/funding/prod/system.jsonc \
		--from-file=configs/funding/prod/exchange.jsonc \
		--from-file=configs/funding/prod/funding.jsonc \
		--from-file=configs/funding/prod/blacklist.jsonc \
		--from-file=configs/funding/prod/reversion.jsonc \
		--from-file=configs/funding/prod/obfuscator.jsonc \
		--from-file=configs/funding/prod/dilution.jsonc \
		-n default -o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/funding -n default

.PHONY: apply-pj-configs
apply-pj-configs: ## Hot-reload Penny Jumper configurations to running K8s cluster
	kubectl create configmap penny-jumper-configs \
		--from-file=configs/penny_jumper/prod/system.jsonc \
		--from-file=configs/penny_jumper/prod/exchange.jsonc \
		--from-file=configs/penny_jumper/prod/penny_jumper.jsonc \
		--from-file=configs/penny_jumper/prod/blacklist.jsonc \
		-n default -o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/penny-jumper -n default

.PHONY: restart-fd
restart-fd: ## Restart the Funding Bot deployment
	kubectl rollout restart deployment/funding -n default

.PHONY: restart-pj
restart-pj: ## Restart the Penny Jumper deployment
	kubectl rollout restart deployment/penny-jumper -n default

.PHONY: logs/fd
logs/fd: ## Tail live logs for Funding Bot pod
	kubectl logs -f -l bot_type=funding --tail=100 -n default

.PHONY: logs/pj
logs/pj: ## Tail live logs for Penny Jumper pod
	kubectl logs -f -l bot_type=penny_jumper --tail=100 -n default

.PHONY: logs/proxy
logs/proxy: ## Tail live logs for AI Proxy pod
	kubectl logs -f -l app=ai-proxy --tail=100 -n default

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
	@awk 'BEGIN {FS = ":.*?## "} /^[a-zA-Z_\/-]+:.*?## / {printf "  %-15s %s\n", $$1, $$2}' $(MAKEFILE_LIST)
	@echo ""

.DEFAULT_GOAL := help
