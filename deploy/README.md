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
1. **[postgres.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/datasources/postgres.yaml)**: Automatically registers the PostgreSQL datasource.
2. **[provider.yaml](file:///home/four/projects/crypto-bot/deploy/grafana/provisioning/dashboards/provider.yaml)**: Directs Grafana to read local dashboard JSON configurations.
3. **[pnl-analytics.json](file:///home/four/projects/crypto-bot/deploy/grafana/dashboards/pnl-analytics.json)**: The pre-configured dashboard JSON template.

### 4. P&L Analytics Dashboard Panels

The `Crypto-Bot Trade P&L Analytics` dashboard contains 19 pre-configured panels to provide comprehensive operational and performance visibility. Each panel queries the PostgreSQL database (table `reversion_trade_reports`), respecting variables (`$exchange` and `$symbol`) to dynamically filter data:

#### Key Performance Indicators (KPIs)
- **Total Net Profit (USD)**
  * **Type**: Stat
  * **Query**: `SELECT COALESCE(SUM(net_profit), 0) FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Win Rate (%)**
  * **Type**: Stat
  * **Query**: `SELECT (COUNT(CASE WHEN net_profit > 0 THEN 1 END)::FLOAT / NULLIF(COUNT(CASE WHEN status = 'completed' THEN 1 END), 0)) * 100 FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Total Completed Trades**
  * **Type**: Stat
  * **Query**: `SELECT COUNT(*) FROM reversion_trade_reports WHERE status = 'completed' AND exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Total Volume Traded (USDT)**
  * **Type**: Stat
  * **Query**: `SELECT COALESCE(SUM(volume_usdt), 0) FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Total Funding Captured (USD)**
  * **Type**: Stat
  * **Query**: `SELECT COALESCE(SUM(hold_fee), 0) FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Total Fee (USD)**
  * **Type**: Stat
  * **Query**: `SELECT COALESCE(SUM(fee), 0) FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`

#### Execution Diagnostics & Latency
- **Hourly/Daily P&L Trend**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), COALESCE(SUM(net_profit), 0) AS net_profit FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY 1 ORDER BY 1`
- **Equity Growth Curve (Cumulative)**
  * **Type**: Time Series
  * **Query**: `SELECT timestamp, SUM(net_profit) OVER (ORDER BY timestamp ASC) AS cumulative_profit FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) ORDER BY timestamp ASC`
- **Execution Latency (RTT)**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(latency_rtt_ms) AS avg_latency FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) AND latency_rtt_ms IS NOT NULL GROUP BY 1 ORDER BY 1`
- **Average Actual Slippage (%)**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(actual_slippage) AS avg_slippage FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) AND actual_slippage IS NOT NULL GROUP BY 1 ORDER BY 1`
- **Target Buffer vs. Actual Fire Offset (ms)**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), exchange, AVG(buffer_time_ms) AS target_buffer_ms, AVG(fire_offset_ms) AS actual_fire_offset_ms FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
- **Funding Fee Avoidance vs. Fire Offset**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), exchange, AVG(fire_offset_ms) AS avg_fire_offset_ms, SUM(hold_fee) AS total_funding_fee_usd FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
- **Traded Funding Rates (%)**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), normalized_symbol, AVG(funding_rate) * 100 AS avg_funding_rate FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
- **IOC Order Fill Ratio**
  * **Type**: Pie Chart
  * **Query**: `SELECT ioc_outcome, COUNT(*) FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND ioc_outcome IS NOT NULL AND ioc_outcome <> '' AND $__timeFilter(timestamp) GROUP BY ioc_outcome`
- **IOC Cancel Rate (%)**
  * **Type**: Stat
  * **Query**: `SELECT (COUNT(CASE WHEN ioc_outcome = 'canceled_no_fill' THEN 1 END)::FLOAT / NULLIF(COUNT(CASE WHEN ioc_order_id IS NOT NULL AND ioc_order_id <> '' THEN 1 END), 0)) * 100 AS cancel_rate FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp)`
- **Trade Hold Duration**
  * **Type**: Time Series
  * **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(hold_duration_ms) AS avg_hold_duration FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) AND hold_duration_ms IS NOT NULL GROUP BY 1 ORDER BY 1`

#### Exit & Abort Breakdown
- **Position Exit Reason Breakdown**
  * **Type**: Pie Chart
  * **Query**: `SELECT exit_reason, COUNT(*) FROM reversion_trade_reports WHERE status = 'completed' AND exit_reason IS NOT NULL AND exit_reason <> '' AND exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY exit_reason`
- **Abort Reasons Breakdown**
  * **Type**: Pie Chart
  * **Query**: `SELECT ioc_reason, COUNT(*) FROM reversion_trade_reports WHERE status = 'aborted' AND ioc_reason IS NOT NULL AND ioc_reason <> '' AND exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) GROUP BY ioc_reason`

#### Historical Ledger
- **Live Trade History Ledger Table**
  * **Type**: Table
  * **Query**: `SELECT timestamp, exchange, normalized_symbol, side, margin_usdt, leverage, fill_price, close_price, net_profit, hold_duration_ms, status, exit_reason, error_msg FROM reversion_trade_reports WHERE exchange = ANY($exchange) AND normalized_symbol = ANY($symbol) AND $__timeFilter(timestamp) ORDER BY timestamp DESC`


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
