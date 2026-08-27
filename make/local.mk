# ──────────────────────────────────────────────────────────────────────
# crypto-bot — Local Development & Runner Targets
# Included by root Makefile (make/local.mk)
# ──────────────────────────────────────────────────────────────────────

FUNDING_SYS        ?= ./configs/funding/local/system.jsonc
FUNDING_EXCH       ?= ./configs/funding/local/exchange.jsonc
FUNDING_BOT        ?= ./configs/funding/local/funding.jsonc
FUNDING_BLK        ?= ./configs/funding/local/blacklist.jsonc
FUNDING_REV        ?= ./configs/funding/local/reversion.jsonc
FUNDING_OBF        ?= ./configs/funding/local/obfuscator.jsonc
FUNDING_DIL        ?= ./configs/funding/local/dilution.jsonc

PENNY_JUMPER_SYS   ?= ./configs/penny_jumper/local/system.jsonc
PENNY_JUMPER_EXCH  ?= ./configs/penny_jumper/local/exchange.jsonc
PENNY_JUMPER_BOT   ?= ./configs/penny_jumper/local/penny_jumper.jsonc
PENNY_JUMPER_BLK   ?= ./configs/penny_jumper/local/blacklist.jsonc

# ── Local Run ─────────────────────────────────────────────────────────
.PHONY: run/funding
run/funding: ## Run Funding Bot with local configuration
	$(GO) run ./cmd/funding -sys $(FUNDING_SYS) -exch $(FUNDING_EXCH) -bot $(FUNDING_BOT) -blacklist $(FUNDING_BLK) -reversion $(FUNDING_REV) -obfuscator $(FUNDING_OBF) -dilution $(FUNDING_DIL)

.PHONY: run/penny-jumper
run/penny-jumper: ## Run Penny Jumper Bot with local configuration
	$(GO) run ./cmd/penny_jumper -sys $(PENNY_JUMPER_SYS) -exch $(PENNY_JUMPER_EXCH) -bot $(PENNY_JUMPER_BOT) -blacklist $(PENNY_JUMPER_BLK)

.PHONY: run/penny_jumper
run/penny_jumper: run/penny-jumper ## Alias for run/penny-jumper

.PHONY: scan/funding
scan/funding: ## Scan funding rates across supported futures exchanges. Usage: make scan/funding [exchanges=binance,bybit] [minFundingRate=0.1] [minVol=1000000]
	$(GO) run ./tools/scanner $(if $(exchanges),-exchanges $(exchanges),-exchanges toobit,mexc) $(if $(minFundingRate),-minFundingRate $(minFundingRate),) $(if $(minVol),-minVol $(minVol),)

# ── Local Docker Compose Environment ──────────────────────────────────
.PHONY: dev-up
dev-up: ## Start the local development environment using Docker Compose (with rebuild)
	docker compose up -d --build

.PHONY: dev-infra
dev-infra: ## Start only local infrastructure (PostgreSQL, Grafana & AI Proxy, excluding bot)
	docker compose up -d postgres grafana proxy

.PHONY: dev-down
dev-down: ## Stop the local development environment
	docker compose down

.PHONY: dev-logs
dev-logs: ## Watch logs from all local development containers
	docker compose logs -f

.PHONY: dev-ps
dev-ps: ## List running local development containers
	docker compose ps

# ── Local AI Proxy (CLIProxyAPI) ──────────────────────────────────────
.PHONY: proxy
proxy: ## Start dedicated AI proxy container locally
	docker compose up -d proxy

.PHONY: proxy-logs
proxy-logs: ## Watch logs from local AI proxy container
	docker compose logs -f proxy

.PHONY: proxy-down
proxy-down: ## Stop local AI proxy container
	docker compose stop proxy
