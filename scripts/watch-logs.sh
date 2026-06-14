#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
DEPLOYMENT_NAME="crypto-bot"
CONTAINER_NAME="bot"

echo "=========================================================="
echo "👀 Watching live Go bot logs..."
echo "=========================================================="
echo "Press Ctrl+C to stop."
echo ""

kubectl logs -f deployment/"$DEPLOYMENT_NAME" -c "$CONTAINER_NAME" -n "$NAMESPACE" --tail=100
