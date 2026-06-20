# Local Kubernetes Deployment (K3d / Minikube)

This directory contains the configurations and scripts to deploy the `crypto-bot` trading application along with a **Loki Stack** (Loki + Promtail + Grafana) for logging and P&L metrics tracking onto a local Kubernetes environment.

---

## Prerequisites & Installation (Ubuntu)

You can automatically install all dependencies (**Docker**, **K3d**, **Terraform**, and **Kubectl**) on Ubuntu/Debian systems by running our environment installer script:
```bash
./scripts/setup-env-ubuntu.sh
```

If installing manually, ensure you have the following tools set up:
1. **Docker** (or Docker Desktop)
2. **K3d** (or Minikube)
3. **Terraform** (>= 1.0)
4. **Kubectl**

---

## Deployment Instructions

### Step 1: Start your Local Kubernetes Cluster
We recommend using **K3d** as it starts up instantly and has a very small memory footprint (<512MB RAM):
```bash
# Create local K8s cluster mapping port 8080 for Grafana
k3d cluster create cryptobot-cluster -p "8080:80@loadbalancer"
```

### Step 2: Build the Container Image
The bot's configurations are baked directly into the Docker image during the build phase:
```bash
# Build the image locally
make docker-build

# Import the image into the K3d cluster registry
k3d image import crypto-bot:latest -c cryptobot-cluster

k3d image import ghcr.io/dangnmh/crypto-bot:latest -c cryptobot-cluster
```

### Step 3: Deploy via Terraform
Initialize and apply the Terraform configurations to automatically deploy Vault, the Vault Secrets Operator, the PostgreSQL database, and the bot configuration resources:
```bash
# Initialize Terraform
make tf-init

# Apply the infrastructure deployment
make tf-apply
```

### Step 4: Configure and Seed Vault
Since the bot fetches its credentials from Vault at runtime, you must initialize the Vault Kubernetes auth backend, create policies/roles, and seed the secrets (such as exchange API keys):
```bash
# Run the Vault bootstrapping and initialization script
./deploy/k8s/vault-init.sh
```
The Vault Secrets Operator will automatically sync the credentials from Vault to a local Kubernetes Secret (`crypto-bot-vault-secrets`), injecting them directly into the bot's environment variables.

### Step 5: Access Vault Web UI (Optional)
If you want to inspect or manage the secret values through the Vault Web User Interface, port-forward the Vault service port:
```bash
# Port-forward the Vault service on port 8200
kubectl port-forward svc/vault 8200:8200
```
Open your browser and navigate to `http://localhost:8200`. You can log in using:
* **Method**: Token
* **Token**: `root`

---

## Configuration & Secret Hot-Reloading

### 1. Reloading System Configurations (ConfigMaps)
Instead of rebuilding the entire Docker image when local configurations (`configs/funding/system.jsonc` or `configs/funding/funding.jsonc`) change, you can apply them instantly using Terraform:
```bash
make tf-apply-bot
```
This updates the Kubernetes ConfigMap and triggers a rolling restart of the bot pod automatically in 1–2 seconds.

### 2. Reloading Vault Secrets (Manual Restart)
When you update secrets in Vault (e.g., by re-running the bootstrap scripts or editing them in the Vault Web UI), the **Vault Secrets Operator** will automatically sync the new values into the Kubernetes Secret (`crypto-bot-vault-secrets`).

Since the application loads secrets as environment variables on startup, you must restart the bot manually to apply the changes:
```bash
make restart-bot
```

---

## Monitoring and Dashboards

### 1. Retrieve Grafana Admin Password
```bash
kubectl get secret --namespace default loki-stack-grafana -o jsonpath="{.data.admin-password}" | base64 --decode ; echo
```

### 2. Access Grafana Dashboard
Port-forward the Grafana service to access it on `http://localhost:3000`:
```bash
kubectl port-forward svc/loki-stack-grafana 3000:80
```

### 3. Dedicated Grafana Provisioning
The `deploy/grafana/` directory holds configurations to automatically provision Grafana on startup:
1. **[postgres.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/datasources/postgres.yaml)**: Automatically registers the PostgreSQL datasource.
2. **[provider.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/dashboards/provider.yaml)**: Directs Grafana to read local dashboard JSON configurations.
3. **[pnl-analytics.json](file:///home/four/projects/crypto-bot/deploy/grafana/dashboards/pnl-analytics.json)**: The pre-configured dashboard JSON template.

### 4. P&L Analytics Dashboard Panels

For detailed information on the 19 pre-configured panels, visualization types, and raw SQL queries used by the metrics dashboard, please refer to the dedicated [DASHBOARD.md](file:///home/four/projects/crypto-bot/deploy/DASHBOARD.md) guide.


---

## Viewing Live Logs

To tail the live container logs of the running Go application directly from your terminal:

```bash
make logs
```
This runs the logs watching helper script [scripts/watch-logs.sh](file:///home/four/projects/crypto-bot/scripts/watch-logs.sh) to query the pod and stream logs.

---

## Tear Down & Cleanup

We provide two distinct options for destroying the deployment depending on whether you want to preserve or delete your historical log data:

### Option A: Destroy Bot Only (Keep Logs & Metrics)
This tears down the Go bot deployment and its secrets, but leaves the Loki/Grafana stack and all historical volume data running.
```bash
make destroy-bot
```

### Option B: Destroy Everything (Purge All Resources & Data)
This uninstalls the Loki/Grafana stack, the bot, and permanently deletes all persistent volume storage claims (PVCs) containing historical logs.
```bash
make destroy-all
```

```
ssh -v -p 2222 -N -L 39373:127.0.0.1:39373 dangnmh@26.128.244.94
export KUBECONFIG=./deploy/k8s/k3d-kubeconfig.yaml


kubectl port-forward svc/loki-stack-grafana 3000:80
kubectl port-forward svc/crypto-bot 3100:3100
kubectl port-forward svc/postgresql 5432:5432

```