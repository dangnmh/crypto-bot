#!/bin/bash
set -e

echo "Waiting for vault pod to be ready..."
kubectl wait --for=condition=Ready pod/vault-0 --timeout=120s

echo "Initializing and configuring Vault K8s auth backend..."

# Run commands inside the vault-0 pod using kubectl exec
kubectl exec -i vault-0 -- env VAULT_TOKEN="${VAULT_TOKEN:-root}" sh <<'EOF'
export VAULT_ADDR="http://127.0.0.1:8200"

# Enable Kubernetes Auth if not already enabled
if ! vault auth list | grep -q "kubernetes/"; then
    vault auth enable kubernetes
fi

# Configure Kubernetes Auth config
vault write auth/kubernetes/config \
    kubernetes_host="https://kubernetes.default.svc.cluster.local:443"

# Create policy for crypto-bot
vault policy write crypto-bot-policy - <<'POL'
path "secret/data/crypto-bot" {
  capabilities = ["read"]
}
path "secret/data/crypto-bot/*" {
  capabilities = ["read"]
}
POL

# Bind role for service account crypto-bot in default namespace
vault write auth/kubernetes/role/crypto-bot-role \
    bound_service_account_names=crypto-bot \
    bound_service_account_namespaces=default \
    policies=crypto-bot-policy \
    ttl=24h

# Populate secret key-value pairs
vault kv put secret/crypto-bot \
  MEXC_API_KEY="mexc_api_key_from_vault" \
  MEXC_API_SECRET="mexc_api_secret_from_vault" \
  GATE_API_KEY="gate_api_key_from_vault" \
  GATE_API_SECRET="gate_api_secret_from_vault" \
  OKX_API_KEY="okx_api_key_from_vault" \
  OKX_API_SECRET="okx_api_secret_from_vault" \
  OKX_API_PASSPHRASE="okx_api_passphrase_from_vault" \
  BYBIT_API_KEY="bybit_api_key_from_vault" \
  BYBIT_API_SECRET="bybit_api_secret_from_vault" \
  BINANCE_API_KEY="binance_api_key_from_vault" \
  BINANCE_API_SECRET="binance_api_secret_from_vault" \
  BITGET_API_KEY="bitget_api_key_from_vault" \
  BITGET_API_SECRET="bitget_api_secret_from_vault" \
  KUCOIN_API_KEY="kucoin_api_key_from_vault" \
  KUCOIN_API_SECRET="kucoin_api_secret_from_vault" \
  KUCOIN_API_PASSPHRASE="kucoin_api_passphrase_from_vault" \
  BINGX_API_KEY="bingx_api_key_from_vault" \
  BINGX_API_SECRET="bingx_api_secret_from_vault" \
  DATABASE_URL="postgres://postgres:postgres@postgresql:5432/postgres?sslmode=disable" \
  TELEGRAM_CHAT_ID="telegram_chat_id_from_vault" \
  TELEGRAM_BOT_TOKEN="telegram_bot_token_from_vault"

echo "Vault configuration complete."
EOF

echo "Applying Vault secret synchronization manifests..."
kubectl apply -f "$(dirname "$0")/vault-secret-sync.yaml"

