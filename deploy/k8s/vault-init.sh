#!/bin/bash
set -e

# Path to the keys file
KEYS_FILE="$(dirname "$0")/vault-keys.json"

echo "Waiting for vault-0 pod to be running..."
# Wait for the pod to be in the "Running" phase (sealed pod won't reach "Ready" state)
for i in {1..60}; do
  PHASE=$(kubectl get pod vault-0 -o jsonpath='{.status.phase}' 2>/dev/null || true)
  if [ "$PHASE" = "Running" ]; then
    echo "vault-0 pod is running."
    break
  fi
  if [ $i -eq 60 ]; then
    echo "Timed out waiting for vault-0 pod to run."
    exit 1
  fi
  sleep 2
done

# Wait for vault port to be responsive (vault status command works)
echo "Waiting for vault API inside container to respond..."
for i in {1..30}; do
  if kubectl exec vault-0 -- vault status -format=json >/dev/null 2>&1; STATUS_EXIT=$?; [ $STATUS_EXIT -eq 0 ] || [ $STATUS_EXIT -eq 2 ]; then
    echo "Vault API is responding."
    break
  fi
  if [ $i -eq 30 ]; then
    echo "Timed out waiting for Vault API to respond."
    exit 1
  fi
  sleep 2
done

# Retrieve Vault status
INIT_STATUS=$(kubectl exec vault-0 -- vault status -format=json 2>/dev/null || true)
IS_INIT=$(echo "$INIT_STATUS" | jq -r '.initialized' 2>/dev/null || true)

# 1. Initialize Vault if not initialized
if [ "$IS_INIT" != "true" ]; then
  echo "Vault is not initialized. Initializing..."
  INIT_OUT=$(kubectl exec vault-0 -- vault operator init -key-shares=1 -key-threshold=1 -format=json)
  echo "$INIT_OUT" > "$KEYS_FILE"
  echo "Initialization keys saved to $KEYS_FILE."
fi

# 2. Extract unseal key and root token from the keys file
if [ ! -f "$KEYS_FILE" ]; then
  echo "Error: Vault is initialized, but $KEYS_FILE is missing. Cannot unseal."
  exit 1
fi

UNSEAL_KEY=$(jq -r '.unseal_keys_b64[0]' "$KEYS_FILE")
ROOT_TOKEN=$(jq -r '.root_token' "$KEYS_FILE")

# 3. Unseal Vault if sealed
INIT_STATUS=$(kubectl exec vault-0 -- vault status -format=json 2>/dev/null || true)
IS_SEALED=$(echo "$INIT_STATUS" | jq -r '.sealed' 2>/dev/null || true)
if [ "$IS_SEALED" = "true" ]; then
  echo "Vault is sealed. Unsealing..."
  kubectl exec vault-0 -- vault operator unseal "$UNSEAL_KEY"
fi

# Extract vault_password from terraform.tfvars
TF_VARS_FILE="$(dirname "$0")/../terraform/terraform.tfvars"
VAULT_PASSWORD=""
if [ -f "$TF_VARS_FILE" ]; then
  VAULT_PASSWORD=$(grep -E '^\s*vault_password\s*=' "$TF_VARS_FILE" | sed -E 's/.*=\s*"([^"]*)".*/\1/' || true)
fi
VAULT_PASSWORD="${VAULT_PASSWORD:-root}"

echo "Vault is unsealed. Configuring and seeding Vault..."

# Use the root token extracted from the keys file
export VAULT_TOKEN="$ROOT_TOKEN"

# Run commands inside the vault-0 pod using kubectl exec
kubectl exec -i vault-0 -- env VAULT_TOKEN="$VAULT_TOKEN" VAULT_PASSWORD="$VAULT_PASSWORD" sh <<'EOF'
export VAULT_ADDR="http://127.0.0.1:8200"

# Revoke existing custom root token if it exists and create it anew
vault token revoke "$VAULT_PASSWORD" 2>/dev/null || true
vault token create -id="$VAULT_PASSWORD" -policy="root" >/dev/null

# Enable Key-Value v2 engine at 'secret' path if not enabled
if ! vault secrets list | grep -q "secret/"; then
    vault secrets enable -path=secret kv-v2
fi

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
  TOOBIT_API_KEY="toobit_api_key_from_vault" \
  TOOBIT_API_SECRET="toobit_api_secret_from_vault" \
  BITMART_API_KEY="bitmart_api_key_from_vault" \
  BITMART_API_SECRET="bitmart_api_secret_from_vault" \
  XT_API_KEY="xt_api_key_from_vault" \
  XT_API_SECRET="xt_api_secret_from_vault" \
  BITMART_API_PASSPHRASE="bitmart_api_passphrase_from_vault" \
  WEEX_API_KEY="weex_api_key_from_vault" \
  WEEX_API_SECRET="weex_api_secret_from_vault" \
  WEEX_API_PASSPHRASE="weex_api_passphrase_from_vault" \
  BITUNIX_API_KEY="bitunix_api_key_from_vault" \
  BITUNIX_API_SECRET="bitunix_api_secret_from_vault" \
  DATABASE_URL="postgres://postgres:postgres@postgresql:5432/postgres?sslmode=disable" \
  TELEGRAM_CHAT_ID="telegram_chat_id_from_vault" \
  TELEGRAM_BOT_TOKEN="telegram_bot_token_from_vault"

echo "Vault configuration complete."
EOF

echo "Applying Vault secret synchronization manifests..."
kubectl apply -f "$(dirname "$0")/vault-secret-sync.yaml"
