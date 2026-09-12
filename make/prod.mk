# ── Multi-Server Configuration & Routing ──────────────────────────────
# Default server is 'prod' (configs/funding/prod, k3d-kubeconfig.yaml)
# Use server=sg for Singapore server (configs/funding/prod-sg, k3s-sg.yaml)
server ?= prod

ifeq ($(filter sg prod-sg,$(server)),$(server))
    TARGET_KUBECONFIG ?= deploy/k8s/k3s-sg.yaml
    FD_CONFIG_DIR     ?= configs/funding/prod-sg
    PJ_CONFIG_DIR     ?= configs/penny_jumper/prod-sg
    TFVARS_FLAG       ?= $(if $(wildcard deploy/terraform/terraform.sg.tfvars),-var-file=terraform.sg.tfvars,)
else
    TARGET_KUBECONFIG ?= deploy/k8s/k3d-kubeconfig.yaml
    FD_CONFIG_DIR     ?= configs/funding/prod
    PJ_CONFIG_DIR     ?= configs/penny_jumper/prod
    TFVARS_FLAG       ?= $(if $(wildcard deploy/terraform/terraform.tfvars),-var-file=terraform.tfvars,)
endif

ACTIVE_KUBECONFIG := $(if $(kubeconfig),$(kubeconfig),$(TARGET_KUBECONFIG))
KCTL_ENV          := $(if $(wildcard $(ACTIVE_KUBECONFIG)),KUBECONFIG=$(ACTIVE_KUBECONFIG),)
KCTL              := $(KCTL_ENV) kubectl

# ── Terraform Multi-Server Workspace & Runner ─────────────────────────
TF_DIR := deploy/terraform
TF_WS  := $(if $(filter sg prod-sg,$(server)),sg,default)
TF_RUN = @terraform -chdir=$(TF_DIR) workspace select $(TF_WS) >/dev/null 2>&1 || terraform -chdir=$(TF_DIR) workspace new $(TF_WS) >/dev/null 2>&1; terraform -chdir=$(TF_DIR)

# ── Terraform Infrastructure & Deployments ────────────────────────────
.PHONY: tf-init
tf-init: ## Initialize Terraform providers and backend (Usage: make tf-init [server=sg])
	terraform -chdir=$(TF_DIR) init

.PHONY: tf-apply
tf-apply: ## Apply all Terraform configurations (Usage: make tf-apply [server=sg])
	$(TF_RUN) apply $(TFVARS_FLAG)

.PHONY: tf-destroy
tf-destroy: ## Destroy all Terraform configurations (Usage: make tf-destroy [server=sg])
	$(TF_RUN) destroy $(TFVARS_FLAG)

.PHONY: tf-apply-bots
tf-apply-bots: ## Apply changes to all trading bot deployments (Usage: make tf-apply-bots [server=sg])
	$(TF_RUN) apply $(TFVARS_FLAG) \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-apply-bot
tf-apply-bot: ## Apply Terraform changes to a specific bot (Usage: make tf-apply-bot bot=NAME [server=sg])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make tf-apply-bot bot=funding [server=sg])"; exit 1; fi
	$(TF_RUN) apply $(TFVARS_FLAG) \
		-target='kubernetes_deployment_v1.bot["$(bot)"]' \
		-target='kubernetes_config_map_v1.bot_configs["$(bot)"]' \
		-target='kubernetes_service_v1.bot["$(bot)"]'

.PHONY: tf-destroy-bots
tf-destroy-bots: ## Destroy all trading bot deployments (Usage: make tf-destroy-bots [server=sg])
	$(TF_RUN) destroy $(TFVARS_FLAG) \
		-target=kubernetes_deployment_v1.bot \
		-target=kubernetes_config_map_v1.bot_configs \
		-target=kubernetes_service_v1.bot

.PHONY: tf-destroy-bot
tf-destroy-bot: ## Destroy Terraform resources for a specific bot (Usage: make tf-destroy-bot bot=NAME [server=sg])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make tf-destroy-bot bot=funding [server=sg])"; exit 1; fi
	$(TF_RUN) destroy $(TFVARS_FLAG) \
		-target='kubernetes_deployment_v1.bot["$(bot)"]' \
		-target='kubernetes_config_map_v1.bot_configs["$(bot)"]' \
		-target='kubernetes_service_v1.bot["$(bot)"]'

.PHONY: tf-apply-proxy
tf-apply-proxy: ## Apply changes only to dedicated AI proxy deployment (Usage: make tf-apply-proxy [server=sg])
	$(TF_RUN) apply $(TFVARS_FLAG) \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy \
		-target=kubernetes_secret_v1.ai_proxy_secrets

.PHONY: tf-destroy-proxy
tf-destroy-proxy: ## Destroy only dedicated AI proxy deployment (Usage: make tf-destroy-proxy [server=sg])
	$(TF_RUN) destroy $(TFVARS_FLAG) \
		-target=kubernetes_deployment_v1.ai_proxy \
		-target=kubernetes_config_map_v1.ai_proxy_config \
		-target=kubernetes_service_v1.ai_proxy \
		-target=kubernetes_secret_v1.ai_proxy_secrets

.PHONY: tf-apply-infra
tf-apply-infra: ## Apply only core infrastructure (Usage: make tf-apply-infra [server=sg])
	$(TF_RUN) apply $(TFVARS_FLAG) \
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
tf-destroy-infra: ## Destroy only core infrastructure (Usage: make tf-destroy-infra [server=sg])
	$(TF_RUN) destroy $(TFVARS_FLAG) \
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
destroy-bot: ## Destroy trading bot deployment (Usage: make destroy-bot [bot=NAME] [fast=true] [server=sg])
	@chmod +x scripts/destroy-bot.sh
	@terraform -chdir=$(TF_DIR) workspace select $(TF_WS) >/dev/null 2>&1 || true
	KUBECONFIG=$(ACTIVE_KUBECONFIG) ./scripts/destroy-bot.sh $(bot) $(if $(filter true 1 yes,$(fast)),--fast,)

.PHONY: destroy-all
destroy-all: ## Destroy entire cluster stack including Go bots, monitoring, and database (Usage: make destroy-all [server=sg])
	@chmod +x scripts/destroy-all.sh
	@terraform -chdir=$(TF_DIR) workspace select $(TF_WS) >/dev/null 2>&1 || true
	KUBECONFIG=$(ACTIVE_KUBECONFIG) ./scripts/destroy-all.sh

.PHONY: destroy-pgsql
destroy-pgsql: ## Destroy PostgreSQL deployment resources, configs, and storage volume (Usage: make destroy-pgsql [server=sg])
	@chmod +x scripts/destroy-pgsql.sh
	@terraform -chdir=$(TF_DIR) workspace select $(TF_WS) >/dev/null 2>&1 || true
	KUBECONFIG=$(ACTIVE_KUBECONFIG) ./scripts/destroy-pgsql.sh

# ── Production Hot-Reloading & Pod Management ─────────────────────────
.PHONY: apply-bot-configs
apply-bot-configs: ## Hot-reload configs for a specific bot (Usage: make apply-bot-configs bot=NAME [server=sg] [dir=PATH])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make apply-bot-configs bot=funding [server=sg])"; exit 1; fi
	@BASE_TYPE=$$(echo $(bot) | sed 's/-.*//'); \
	DIR=$$(if [ -n "$(dir)" ]; then echo "$(dir)"; else \
		if [ -d "configs/$$BASE_TYPE/prod-$(server)" ]; then echo "configs/$$BASE_TYPE/prod-$(server)"; \
		else echo "configs/$$BASE_TYPE/prod"; fi; \
	fi); \
	echo "==> Applying ConfigMap $(bot)-configs to server [$(server)] from $$DIR (Kubeconfig: $(ACTIVE_KUBECONFIG))..."; \
	$(KCTL) create configmap $(bot)-configs \
		--from-file="$$DIR" \
		-n default -o yaml --dry-run=client | $(KCTL) apply -f -
	$(KCTL) rollout restart deployment/$(bot) -n default

.PHONY: apply-fd-configs
apply-fd-configs: ## Hot-reload Funding Bot configs to K8s (Usage: make apply-fd-configs [server=sg] [dir=PATH])
	@CFG_DIR=$$(if [ -n "$(dir)" ]; then echo "$(dir)"; else echo "$(FD_CONFIG_DIR)"; fi); \
	echo "==> Applying Funding Bot configs to server [$(server)] from $$CFG_DIR (Kubeconfig: $(ACTIVE_KUBECONFIG))..."; \
	$(KCTL) create configmap funding-configs \
		--from-file="$$CFG_DIR/system.jsonc" \
		--from-file="$$CFG_DIR/exchange.jsonc" \
		--from-file="$$CFG_DIR/funding.jsonc" \
		--from-file="$$CFG_DIR/blacklist.jsonc" \
		--from-file="$$CFG_DIR/reversion.jsonc" \
		--from-file="$$CFG_DIR/obfuscator.jsonc" \
		--from-file="$$CFG_DIR/dilution.jsonc" \
		-n default -o yaml --dry-run=client | $(KCTL) apply -f -
	$(KCTL) rollout restart deployment -l bot_type=funding -n default

.PHONY: apply-pj-configs
apply-pj-configs: ## Hot-reload Penny Jumper configs to K8s (Usage: make apply-pj-configs [server=sg] [dir=PATH])
	@CFG_DIR=$$(if [ -n "$(dir)" ]; then echo "$(dir)"; else echo "$(PJ_CONFIG_DIR)"; fi); \
	echo "==> Applying Penny Jumper configs to server [$(server)] from $$CFG_DIR (Kubeconfig: $(ACTIVE_KUBECONFIG))..."; \
	$(KCTL) create configmap penny-jumper-spot-configs \
		--from-file="$$CFG_DIR/system.jsonc" \
		--from-file="$$CFG_DIR/exchange.jsonc" \
		--from-file="$$CFG_DIR/penny_jumper.jsonc" \
		--from-file="$$CFG_DIR/blacklist.jsonc" \
		-n default -o yaml --dry-run=client | $(KCTL) apply -f -
	$(KCTL) rollout restart deployment -l bot_type=penny_jumper -n default

.PHONY: restart-bot
restart-bot: ## Restart a specific bot deployment in K8s (Usage: make restart-bot bot=NAME [server=sg])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make restart-bot bot=funding [server=sg])"; exit 1; fi
	$(KCTL) rollout restart deployment/$(bot) -n default

.PHONY: restart-fd
restart-fd: ## Restart Funding Bot deployments in K8s (Usage: make restart-fd [server=sg])
	$(KCTL) rollout restart deployment -l bot_type=funding -n default

.PHONY: restart-pj
restart-pj: ## Restart Penny Jumper deployments in K8s (Usage: make restart-pj [server=sg])
	$(KCTL) rollout restart deployment -l bot_type=penny_jumper -n default

# ── Live Production Logs ──────────────────────────────────────────────
.PHONY: logs/bot
logs/bot: ## Tail live logs for a specific bot pod in K8s (Usage: make logs/bot bot=NAME [server=sg])
	@if [ -z "$(bot)" ]; then echo "Error: 'bot' argument required (e.g. make logs/bot bot=funding [server=sg])"; exit 1; fi
	$(KCTL) logs -f -l app=$(bot) --tail=100 -n default

.PHONY: logs/fd
logs/fd: ## Tail live logs for Funding Bot pod in K8s (Usage: make logs/fd [server=sg])
	$(KCTL) logs -f -l bot_type=funding --tail=100 -n default

.PHONY: logs/pj
logs/pj: ## Tail live logs for Penny Jumper pod in K8s (Usage: make logs/pj [server=sg])
	$(KCTL) logs -f -l bot_type=penny_jumper --tail=100 -n default

.PHONY: logs/proxy
logs/proxy: ## Tail live logs for AI Proxy pod in K8s (Usage: make logs/proxy [server=sg])
	$(KCTL) logs -f -l app=ai-proxy --tail=100 -n default
