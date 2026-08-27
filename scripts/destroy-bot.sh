#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
BOT_NAME=""
FAST_MODE=false

# Parse arguments
for arg in "$@"; do
    if [[ "$arg" == "--fast" ]]; then
        FAST_MODE=true
    elif [[ -z "$BOT_NAME" ]]; then
        BOT_NAME="$arg"
    fi
done

if [[ -n "$BOT_NAME" ]]; then
    echo "=========================================================="
    echo "🛑 Destroying Specific Bot: $BOT_NAME"
    echo "=========================================================="
    
    if [ "$FAST_MODE" = true ]; then
        echo "⚡ Using fast kubectl deletion for $BOT_NAME..."
        kubectl delete deployment "$BOT_NAME" -n "$NAMESPACE" --ignore-not-found
        kubectl delete service "$BOT_NAME" -n "$NAMESPACE" --ignore-not-found
        kubectl delete configmap "${BOT_NAME}-configs" -n "$NAMESPACE" --ignore-not-found
        echo "✅ Bot '$BOT_NAME' deleted successfully."
        exit 0
    fi

    echo "⏳ Running targeted Terraform destroy for '$BOT_NAME'..."
    terraform -chdir=deploy/terraform destroy \
        -target="kubernetes_deployment_v1.bot[\"$BOT_NAME\"]" \
        -target="kubernetes_service_v1.bot[\"$BOT_NAME\"]" \
        -target="kubernetes_config_map_v1.bot_configs[\"$BOT_NAME\"]" \
        -auto-approve

    echo "✅ Bot '$BOT_NAME' destroyed cleanly via Terraform."
    exit 0
fi

echo "=========================================================="
echo "🛑 Destroying ALL Trading Bot Deployments"
echo "=========================================================="
echo "This script will tear down all trading bot deployments while"
echo "preserving the PostgreSQL database, Loki / Grafana monitoring stack, and Vault."
echo ""

if [ "$FAST_MODE" = true ]; then
    echo "⚡ Using fast kubectl deletion for all bots..."
    kubectl delete deployment,service,configmap -l bot_type -n "$NAMESPACE" --ignore-not-found
    echo "✅ All bot deployments, services, and configs deleted successfully."
    exit 0
fi

echo "⏳ Running targeted Terraform destroy for all bots..."
terraform -chdir=deploy/terraform destroy \
    -target=kubernetes_deployment_v1.bot \
    -target=kubernetes_service_v1.bot \
    -target=kubernetes_config_map_v1.bot_configs \
    -auto-approve

echo "=========================================================="
echo "✅ All bot deployments and configs destroyed cleanly."
echo "💡 The database and monitoring stack remain intact with all historical data."
echo "=========================================================="
