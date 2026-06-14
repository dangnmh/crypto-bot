#!/usr/bin/env bash

# Exit immediately if a command exits with a non-zero status
set -euo pipefail

NAMESPACE="default"
DEPLOYMENT_NAME="crypto-bot"
LABEL_SELECTOR="app=crypto-bot"

echo "=========================================================="
echo "🔍 Debugging Kubernetes Deployment: $DEPLOYMENT_NAME"
echo "=========================================================="

echo -e "\n1. Checking Cluster Info..."
if ! kubectl cluster-info &>/dev/null; then
    echo "❌ ERROR: Cannot connect to Kubernetes cluster. Is your cluster running?"
    echo "   If using K3d, run: k3d cluster list"
    echo "   To start it: k3d cluster create cryptobot-cluster -p \"8080:80@loadbalancer\""
    exit 1
fi
kubectl cluster-info

echo -e "\n2. Checking Deployment Status..."
kubectl get deployment "$DEPLOYMENT_NAME" -n "$NAMESPACE" || echo "❌ Deployment $DEPLOYMENT_NAME not found!"

echo -e "\n3. Checking Pods Status..."
kubectl get pods -n "$NAMESPACE" -l "$LABEL_SELECTOR" -o wide

echo -e "\n4. Inspecting Pod Details and Events..."
# Find pod names
PODS=$(kubectl get pods -n "$NAMESPACE" -l "$LABEL_SELECTOR" -o jsonpath='{.items[*].metadata.name}')

if [ -z "$PODS" ]; then
    echo "❌ No pods found for selector $LABEL_SELECTOR."
else
    for POD in $PODS; do
        echo "----------------------------------------------------------"
        echo "📋 Details for Pod: $POD"
        echo "----------------------------------------------------------"
        
        # Check current phase/state
        PHASE=$(kubectl get pod "$POD" -n "$NAMESPACE" -o jsonpath='{.status.phase}')
        echo "Phase: $PHASE"
        
        # Get specific container status/reason
        REASON=$(kubectl get pod "$POD" -n "$NAMESPACE" -o jsonpath='{.status.containerStatuses[0].state.waiting.reason}' 2>/dev/null || true)
        if [ -n "$REASON" ]; then
            echo "⚠️ Waiting Reason: $REASON"
            if [ "$REASON" = "ErrImageNeverPull" ] || [ "$REASON" = "ImagePullBackOff" ]; then
                echo -e "\n💡 TIP: The image '$DEPLOYMENT_NAME:latest' is not available in the Kubernetes cluster registry."
                echo "   Ensure you run: make docker-build"
                echo "   And then import it: k3d image import crypto-bot:latest -c cryptobot-cluster"
            fi
        fi
        
        echo -e "\n🔔 Recent Events for Pod $POD:"
        # Get events related to this pod
        kubectl get events -n "$NAMESPACE" --field-selector involvedObject.name="$POD" --sort-by='.metadata.creationTimestamp' -o custom-columns=TIME:.metadata.creationTimestamp,REASON:.reason,MESSAGE:.message | tail -n 10
        
        echo -e "\n📝 Logs from the current run (last 50 lines):"
        kubectl logs "$POD" -n "$NAMESPACE" -c bot --tail=50 || echo "⚠️ Could not retrieve current logs."
        
        echo -e "\n📝 Logs from the previous run (if crashed/restarted):"
        kubectl logs "$POD" -n "$NAMESPACE" -c bot --previous --tail=50 || echo "ℹ️ No previous logs available (pod has not restarted yet or hasn't crashed)."
    done
fi

echo -e "\n5. Checking Secrets..."
if kubectl get secret -n "$NAMESPACE" crypto-bot-secrets &>/dev/null; then
    echo "✅ Secret 'crypto-bot-secrets' is present."
else
    echo "❌ Secret 'crypto-bot-secrets' is missing!"
fi

echo -e "\n6. Checking Local Docker Image..."
if command -v docker &>/dev/null; then
    docker images | grep crypto-bot || echo "⚠️ Local Docker image 'crypto-bot' not found in Docker daemon."
fi

echo "=========================================================="
echo "💡 General Troubleshooting Tips:"
echo "----------------------------------------------------------"
echo "- If stuck on image pulling, import the image: k3d image import crypto-bot:latest -c cryptobot-cluster"
echo "- If stuck on container crash, check logs above. Bitwarden SDK credentials or configs might be missing or incorrect."
echo "- If Terraform is hanging, you can cancel 'make tf-apply' with Ctrl+C. Since Terraform is waiting for the deployment to become ready, canceling it won't destroy the resources already created. You can debug the pods, fix the issue, and then run 'make tf-apply' again."
echo "=========================================================="
