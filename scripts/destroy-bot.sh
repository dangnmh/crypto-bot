#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
DEPLOYMENT_NAME="crypto-bot"

echo "=========================================================="
echo "🛑 Destroying Go Bot Deployment Only"
echo "=========================================================="
echo "This script will tear down the Go bot deployment while"
echo "preserving the Loki / Grafana stack and all historical logs."
echo ""

# Check if a fast kubectl delete is preferred
if [[ "${1:-}" == "--fast" ]]; then
    echo "⚡ Using fast kubectl deletion..."
    kubectl delete deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" --ignore-not-found
    echo "✅ Bot deployment deleted successfully."
    exit 0
fi

# Default: Use Terraform to destroy the targeted resources cleanly
echo "⏳ Running targeted Terraform destroy..."
terraform -chdir=deploy/terraform destroy \
    -target=kubernetes_deployment.crypto_bot \
    -target=kubernetes_secret.crypto_bot_secrets \
    -auto-approve

echo "=========================================================="
echo "✅ Bot deployment and secrets destroyed cleanly."
echo "💡 The Loki stack remains intact with all historical logs."
echo "=========================================================="
