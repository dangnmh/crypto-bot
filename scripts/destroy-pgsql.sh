#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
PGSQL_PVC="data-postgresql-0"

echo "=========================================================="
echo "💥 DESTROYING ALL POSTGRESQL RESOURCES"
echo "=========================================================="
echo "⚠️ WARNING: This will permanently delete the PostgreSQL database"
echo "           deployment, secrets, and all stored data volume!"
echo "=========================================================="
echo ""

# Confirm action if not forced
if [[ "${1:-}" != "--force" ]]; then
    read -p "Are you absolutely sure you want to delete ALL PostgreSQL resources? (y/N): " -r
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "❌ Destruction aborted."
        exit 1
    fi
fi

# 1. Run targeted Terraform destroy for postgresql Helm release
echo "⏳ Running targeted Terraform destroy for PostgreSQL..."
terraform -chdir="$(dirname "$0")/../deploy/terraform" destroy -target=helm_release.postgresql -auto-approve || true

# 2. Delete the persistent volume claim (releasing any remnants)
echo "🧹 Purging PostgreSQL Persistent Volume Claim (PVC)..."
kubectl delete pvc "$PGSQL_PVC" -n "$NAMESPACE" --ignore-not-found

echo "=========================================================="
echo "✅ All PostgreSQL resources have been successfully destroyed."
echo "=========================================================="
