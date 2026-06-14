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
```

### Step 3: Deploy via Terraform
Initialize and apply the Terraform configurations to automatically deploy the secrets, deployment resources, and Loki Stack:
```bash
# Initialize Terraform
make tf-init

# Apply the infrastructure deployment
# This will prompt you to enter your Bitwarden Secrets Manager credentials
make tf-apply
```

---

---

## Configuration Hot-Reloading

Instead of rebuilding the entire Docker image and importing it to the cluster when configurations change, you can modify your configurations (`configs/funding/system.jsonc` and `configs/funding/funding.jsonc`) and apply them instantly:

```bash
make tf-apply-bot
```
This updates the Kubernetes ConfigMap directly from your workspace and triggers a rollout restart of the bot pod to load the new configuration in 1–2 seconds.

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
1. **[loki.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/datasources/loki.yaml)**: Automatically registers the Loki log datasource.
2. **[provider.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/dashboards/provider.yaml)**: Directs Grafana to read local dashboard JSON configurations.
3. **[pnl-analytics.json](file:///home/four/projects/crypto-bot/deploy/grafana/dashboards/pnl-analytics.json)**: The pre-configured dashboard JSON template.

### 4. P&L Analytics Dashboard Panels

The `Crypto-Bot Trade P&L Analytics` dashboard contains 9 pre-configured panels to provide comprehensive operational and performance visibility. Each panel queries the Loki log stream using LogQL to extract structured JSON fields under the `payload` key, and respects the dashboard's variables (`$exchange` and `$symbol`) to dynamically filter data:

#### Stat Panels (Key Performance Indicators)
- **Total Net Profit (USD)** (ID 1)
  * **Type**: Stat
  * **Query**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_net_pnl | __error__="" [$__range]))`
  * **Responsibility**: Tracks aggregate net profitability (revenue minus execution fees and funding payments) across filtered exchanges and symbols.
- **Total Funding Fees** (ID 2)
  * **Type**: Stat
  * **Query**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_hold_fees | __error__="" [$__range]))`
  * **Responsibility**: Tracks aggregate funding fees captured/paid during position holding times.
- **Total Fee** (ID 3)
  * **Type**: Stat
  * **Query**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_fees | __error__="" [$__range]))`
  * **Responsibility**: Accumulates the transaction/execution fees paid to exchanges for opening and closing positions.

#### Time Series Panels (Trend Analysis)
- **Hourly Net P&L** (ID 4)
  * **Type**: Time Series
  * **Query**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_net_pnl | __error__="" [1h]))`
  * **Responsibility**: Visualizes the net profit/loss trend grouped in 1-hour intervals.
- **Hourly Funding Fees** (ID 5)
  * **Type**: Time Series
  * **Query**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_hold_fees | __error__="" [1h]))`
  * **Responsibility**: Tracks funding fee trends in 1-hour intervals.
- **Hourly Net P&L by Exchange** (ID 10)
  * **Type**: Time Series
  * **Query**: `sum by (payload_exchange) (sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_net_pnl | __error__="" [1h]))`
  * **Responsibility**: Tracks and visualizes hourly net profit/loss trends individually for each active exchange, allowing multi-line comparisons.

#### Breakdown Panels (Distribution & Share)
- **PnL & Fee Breakdown** (ID 6)
  * **Type**: Pie Chart
  * **Query A (Net Profit)**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_net_pnl | __error__="" [$__range]))`
  * **Query B (Funding Fees)**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_hold_fees | __error__="" [$__range])) * -1`
  * **Query C (Execution Fees)**: `sum(sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_fees | __error__="" [$__range]))`
  * **Responsibility**: Displays the proportional breakdown of net profits, funding fees, and execution fees relative to one another.
- **Net Profit by Exchange** (ID 7)
  * **Type**: Pie Chart
  * **Query**: `sum by (payload_exchange) (sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_net_pnl | __error__="" [$__range]))`
  * **Responsibility**: Displays the share/distribution of net profits generated across each exchange.
- **Execution Fees by Exchange** (ID 8)
  * **Type**: Pie Chart
  * **Query**: `sum by (payload_exchange) (sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_fees | __error__="" [$__range]))`
  * **Responsibility**: Displays the share/distribution of transaction fees paid across each exchange.
- **Funding Fees by Exchange** (ID 9)
  * **Type**: Pie Chart
  * **Query**: `sum by (payload_exchange) (sum_over_time({app="crypto-bot",topic="funding.reversion.final_pnl"} | json | payload_exchange=~"$exchange" | payload_symbol=~"$symbol" | unwrap payload_hold_fees | __error__="" [$__range])) * -1`
  * **Responsibility**: Displays the share/distribution of funding fees paid/captured across each exchange.


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
