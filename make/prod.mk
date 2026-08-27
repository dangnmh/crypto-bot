# ──────────────────────────────────────────────────────────────────────
# crypto-bot — Production Deployment & Cluster Operations
# Included by root Makefile (make/prod.mk)
# ──────────────────────────────────────────────────────────────────────

# ── Terraform Infrastructure & Deployments ────────────────────────────
.PHONY: tf-init
tf-init: ## Initialize Terraform providers and backend
	terraform -chdir=deploy/terraform init

.PHONY: tf-apply
tf-apply: ## Apply all Terraform configurations (DB, Vault, Monitoring, Bots, Proxy)
	terraform -chdir=deploy/terraform apply

.PHONY: tf-destroy
tf-destroy: ## Destroy all Terraform configurations (keeps PVC data via policy)
	terraform -chdir=deploy/terraform destroy

.PHONY: tf-apply-bots
tf-apply-bots: ## Apply changes to all trading bot deployments
	terraform -chdir=deploy/terraform apply \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-apply-bot
tf-apply-bot: ## Apply Terraform changes to a specific bot (Usage: make tf-apply-bot bot=NAME)
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make tf-apply-bot bot=funding)"; exit 1; fi
	terraform -chdir=deploy/terraform apply \
		-target='kubernetes_deployment_v1.bot["$(bot)"]' \
		-target='kubernetes_config_map_v1.bot_configs["$(bot)"]' \
		-target='kubernetes_service_v1.bot["$(bot)"]'

.PHONY: tf-destroy-bots
tf-destroy-bots: ## Destroy all trading bot deployments, preserving infrastructure & monitoring
	terraform -chdir=deploy/terraform destroy \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-destroy-bot
tf-destroy-bot: ## Destroy Terraform resources for a specific bot (Usage: make tf-destroy-bot bot=NAME)
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make tf-destroy-bot bot=funding)"; exit 1; fi
	terraform -chdir=deploy/terraform destroy \
		-target='kubernetes_deployment_v1.bot["$(bot)"]' \
		-target='kubernetes_config_map_v1.bot_configs["$(bot)"]' \
		-target='kubernetes_service_v1.bot["$(bot)"]'

.PHONY: tf-apply-proxy
tf-apply-proxy: ## Apply changes only to dedicated AI proxy deployment
	terraform -chdir=deploy/terraform apply \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy \
		-target=kubernetes_secret_v1.ai_proxy_secrets

.PHONY: tf-destroy-proxy
tf-destroy-proxy: ## Destroy only dedicated AI proxy deployment
	terraform -chdir=deploy/terraform destroy \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy \
		-target=kubernetes_secret_v1.ai_proxy_secrets

.PHONY: tf-apply-infra
tf-apply-infra: ## Apply only core infrastructure (PostgreSQL, Vault, Loki Stack, Prometheus, Secrets)
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
tf-destroy-infra: ## Destroy only core infrastructure
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

# ── Kubernetes Cluster Operations & Teardown ──────────────────────────
.PHONY: destroy-bot
destroy-bot: ## Destroy trading bot deployment (Usage: make destroy-bot [bot=NAME] [fast=true])
	@chmod +x scripts/destroy-bot.sh
	./scripts/destroy-bot.sh $(bot) $(if $(filter true 1 yes,$(fast)),--fast,)

.PHONY: destroy-all
destroy-all: ## Destroy entire cluster stack including Go bots, monitoring, and database
	@chmod +x scripts/destroy-all.sh
	./scripts/destroy-all.sh

.PHONY: destroy-pgsql
destroy-pgsql: ## Destroy PostgreSQL deployment resources, configs, and storage volume
	@chmod +x scripts/destroy-pgsql.sh
	./scripts/destroy-pgsql.sh

# ── Production Hot-Reloading & Pod Management ─────────────────────────
.PHONY: apply-bot-configs
apply-bot-configs: ## Hot-reload configs for a specific bot (Usage: make apply-bot-configs bot=NAME [dir=PATH])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make apply-bot-configs bot=funding dir=configs/funding/prod)"; exit 1; fi
	@DIR=$$(if [ -n "$(dir)" ]; then echo "$(dir)"; else echo "configs/$$(echo $(bot) | sed 's/-.*//')/prod"; fi); \
	echo "Creating ConfigMap $(bot)-configs from $$DIR..."; \
	kubectl create configmap $(bot)-configs \
		--from-file="$$DIR" \
		-n default -o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment/$(bot) -n default

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
	kubectl rollout restart deployment -l bot_type=funding -n default

.PHONY: apply-pj-configs
apply-pj-configs: ## Hot-reload Penny Jumper configurations to running K8s cluster
	kubectl create configmap penny-jumper-spot-configs \
		--from-file=configs/penny_jumper/prod/system.jsonc \
		--from-file=configs/penny_jumper/prod/exchange.jsonc \
		--from-file=configs/penny_jumper/prod/penny_jumper.jsonc \
		--from-file=configs/penny_jumper/prod/blacklist.jsonc \
		-n default -o yaml --dry-run=client | kubectl apply -f -
	kubectl rollout restart deployment -l bot_type=penny_jumper -n default

.PHONY: restart-bot
restart-bot: ## Restart a specific bot deployment in K8s (Usage: make restart-bot bot=NAME)
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make restart-bot bot=funding)"; exit 1; fi
	kubectl rollout restart deployment/$(bot) -n default

.PHONY: restart-fd
restart-fd: ## Restart Funding Bot deployments in K8s
	kubectl rollout restart deployment -l bot_type=funding -n default

.PHONY: restart-pj
restart-pj: ## Restart Penny Jumper deployments in K8s
	kubectl rollout restart deployment -l bot_type=penny_jumper -n default

# ── Live Production Logs ──────────────────────────────────────────────
.PHONY: logs/bot
logs/bot: ## Tail live logs for a specific bot pod in K8s (Usage: make logs/bot bot=NAME)
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make logs/bot bot=funding)"; exit 1; fi
	kubectl logs -f -l app=$(bot) --tail=100 -n default

.PHONY: logs/fd
logs/fd: ## Tail live logs for Funding Bot pod in K8s
	kubectl logs -f -l bot_type=funding --tail=100 -n default

.PHONY: logs/pj
logs/pj: ## Tail live logs for Penny Jumper pod in K8s
	kubectl logs -f -l bot_type=penny_jumper --tail=100 -n default

.PHONY: logs/proxy
logs/proxy: ## Tail live logs for AI Proxy pod in K8s
	kubectl logs -f -l app=ai-proxy --tail=100 -n default
