#!/usr/bin/env bash

# ==============================================================================
# crypto-bot — Ubuntu Local K8s Environment Setup Script
# Installs: Docker, Kubectl, K3d, and Terraform (>= 1.0)
# ==============================================================================

set -euo pipefail

echo "=================================================="
echo "🚀 Starting Local K8s Environment Setup for Ubuntu"
echo "=================================================="

# Ensure keyring directories exist
sudo mkdir -p /etc/apt/keyrings
sudo chmod 0755 /etc/apt/keyrings

# 1. Install Docker Engine
if ! command -v docker &> /dev/null; then
    echo "📦 Installing Docker Engine..."
    sudo apt-get update
    sudo apt-get install -y ca-certificates curl gnupg lsb-release

    # Add Docker GPG key
    sudo rm -f /etc/apt/keyrings/docker.gpg
    curl -fsSL https://download.docker.com/linux/ubuntu/gpg | sudo gpg --dearmor -o /etc/apt/keyrings/docker.gpg
    sudo chmod a+r /etc/apt/keyrings/docker.gpg

    # Add Docker apt source repository
    echo \
      "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.gpg] https://download.docker.com/linux/ubuntu \
      $(lsb_release -cs) stable" | sudo tee /etc/apt/sources.list.d/docker.list > /dev/null

    sudo apt-get update
    sudo apt-get install -y docker-ce docker-ce-cli containerd.io

    # Allow current user to run Docker without sudo
    sudo usermod -aG docker "$USER"
    echo "✅ Docker installed successfully. (Note: You may need to log out and log back in to run docker without sudo)."
else
    echo "✅ Docker is already installed."
fi

# 2. Install Kubectl
if ! command -v kubectl &> /dev/null; then
    echo "📦 Installing Kubectl..."
    sudo apt-get install -y apt-transport-https

    # Download GPG key for Kubernetes repository
    sudo rm -f /etc/apt/keyrings/kubernetes-apt-keyring.gpg
    curl -fsSL https://pkgs.k8s.io/core:/stable:/v1.29/deb/Release.key | sudo gpg --dearmor -o /etc/apt/keyrings/kubernetes-apt-keyring.gpg
    sudo chmod 644 /etc/apt/keyrings/kubernetes-apt-keyring.gpg

    # Add apt source repository
    echo 'deb [signed-by=/etc/apt/keyrings/kubernetes-apt-keyring.gpg] https://pkgs.k8s.io/core:/stable:/v1.29/deb/ /' | sudo tee /etc/apt/sources.list.d/kubernetes.list

    sudo apt-get update
    sudo apt-get install -y kubectl
    echo "✅ Kubectl installed successfully."
else
    echo "✅ Kubectl is already installed."
fi

# 3. Install K3d
if ! command -v k3d &> /dev/null; then
    echo "📦 Installing K3d (Kubernetes in Docker)..."
    curl -s https://raw.githubusercontent.com/k3d-io/k3d/main/install.sh | TAG=v5.6.0 bash
    echo "✅ K3d installed successfully."
else
    echo "✅ K3d is already installed."
fi

# 4. Install Terraform
if ! command -v terraform &> /dev/null; then
    echo "📦 Installing HashiCorp Terraform..."
    
    # Download HashiCorp GPG key
    sudo rm -f /usr/share/keyrings/hashicorp-archive-keyring.gpg
    wget -O- https://apt.releases.hashicorp.com/gpg | sudo gpg --dearmor -o /usr/share/keyrings/hashicorp-archive-keyring.gpg

    # Add apt source repository
    echo "deb [signed-by=/usr/share/keyrings/hashicorp-archive-keyring.gpg] https://apt.releases.hashicorp.com $(lsb_release -cs) main" | sudo tee /etc/apt/sources.list.d/hashicorp.list

    sudo apt-get update
    sudo apt-get install -y terraform
    echo "✅ Terraform installed successfully."
else
    echo "✅ Terraform is already installed."
fi

echo "=================================================="
echo "🎉 Setup complete! All prerequisites are installed."
echo "=================================================="
