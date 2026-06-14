#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
LOKI_PVC="storage-loki-stack-0"
GRAFANA_PVC="loki-stack-grafana"

echo "=========================================================="
echo "💥 DESTROYING EVERYTHING (Go Bot + Monitoring Stack + Data)"
echo "=========================================================="
echo "⚠️ WARNING: This will permanently delete all logs and dashboards!"
echo "=========================================================="
echo ""

# Confirm action if not forced
if [[ "${1:-}" != "--force" ]]; then
    read -p "Are you absolutely sure you want to delete ALL resources and historical data? (y/N): " -r
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "❌ Destruction aborted."
        exit 1
    fi
fi

# 1. Run full Terraform destroy to uninstall Helm release & deployment
echo "⏳ Running full Terraform destroy..."
terraform -chdir=deploy/terraform destroy -auto-approve

# 2. Delete the orphaned PVCs since they have "helm.sh/resource-policy: keep"
echo "🧹 Purging Persistent Volume Claims (PVCs)..."
kubectl delete pvc "$LOKI_PVC" "$GRAFANA_PVC" -n "$NAMESPACE" --ignore-not-found

echo "=========================================================="
echo "✅ All resources and data have been successfully destroyed."
echo "=========================================================="
