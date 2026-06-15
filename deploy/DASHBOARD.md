# Crypto-Bot Trade P&L Analytics Dashboard

The `Crypto-Bot Trade P&L Analytics` dashboard contains 19 pre-configured panels to provide comprehensive operational and performance visibility. Each panel queries the PostgreSQL database (table `reversion_trade_reports`), respecting variables (`$exchange` and `$symbol`) to dynamically filter data:

### Key Performance Indicators (KPIs)

*   **Total Net Profit (USD)**
    *   **Type**: Stat
    *   **Query**: `SELECT COALESCE(SUM(net_profit), 0) FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **Win Rate (%)**
    *   **Type**: Stat
    *   **Query**: `SELECT COALESCE((COUNT(CASE WHEN net_profit > 0 THEN 1 END)::FLOAT / NULLIF(COUNT(*), 0)) * 100, 0) FROM reversion_trade_reports WHERE status = 'completed' AND exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **Total Completed Trades**
    *   **Type**: Stat
    *   **Query**: `SELECT COUNT(*) FROM reversion_trade_reports WHERE status = 'completed' AND exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **Total Volume Traded (USDT)**
    *   **Type**: Stat
    *   **Query**: `SELECT COALESCE(SUM(volume_usdt), 0) FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **Total Funding Fee (USD)**
    *   **Type**: Stat
    *   **Query**: `SELECT COALESCE(SUM(hold_fee), 0) FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **Total Fee (USD)**
    *   **Type**: Stat
    *   **Query**: `SELECT COALESCE(SUM(fee), 0) FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`

---

### Execution Diagnostics & Latency

*   **Hourly/Daily P&L Trend**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), COALESCE(SUM(net_profit), 0) AS net_profit FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY 1 ORDER BY 1`
*   **Equity Growth Curve (Cumulative)**
    *   **Type**: Time Series
    *   **Query**: `SELECT timestamp AS time, SUM(net_profit) OVER (ORDER BY timestamp ASC) AS cumulative_profit FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) ORDER BY timestamp ASC`
*   **Execution Latency (RTT)**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(latency_rtt_ms) AS avg_latency FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) AND latency_rtt_ms IS NOT NULL GROUP BY 1 ORDER BY 1`
*   **Average Actual Slippage (%)**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(actual_slippage) AS avg_slippage FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) AND actual_slippage IS NOT NULL GROUP BY 1 ORDER BY 1`
*   **Target Buffer vs. Actual Fire Offset (ms)**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), exchange, AVG(buffer_time_ms) AS target_buffer_ms, AVG(fire_offset_ms) AS actual_fire_offset_ms FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
*   **Funding Fee Avoidance vs. Fire Offset**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), exchange, AVG(fire_offset_ms) AS avg_fire_offset_ms, SUM(hold_fee) AS total_funding_fee_usd FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
*   **Traded Funding Rates (%)**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), normalized_symbol, AVG(funding_rate) * 100 AS avg_funding_rate FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY 1, 2 ORDER BY 1`
*   **IOC Order Fill Ratio**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT ioc_outcome, COUNT(*) FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND ioc_outcome IS NOT NULL AND ioc_outcome <> '' AND $__timeFilter(timestamp) GROUP BY ioc_outcome`
*   **IOC Cancel Rate (%)**
    *   **Type**: Stat
    *   **Query**: `SELECT (COUNT(CASE WHEN ioc_outcome = 'canceled_no_fill' THEN 1 END)::FLOAT / NULLIF(COUNT(CASE WHEN ioc_order_id IS NOT NULL AND ioc_order_id <> '' THEN 1 END), 0)) * 100 AS cancel_rate FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp)`
*   **IOC Cancel Rate by Exchange**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT exchange, (COUNT(CASE WHEN ioc_outcome = 'canceled_no_fill' THEN 1 END)::FLOAT / NULLIF(COUNT(CASE WHEN ioc_order_id IS NOT NULL AND ioc_order_id <> '' THEN 1 END), 0)) * 100 AS cancel_rate FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY exchange`
*   **Trade Hold Duration**
    *   **Type**: Time Series
    *   **Query**: `SELECT $__timeGroupAlias(timestamp, $__interval), AVG(hold_duration_ms) AS avg_hold_duration FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) AND hold_duration_ms IS NOT NULL GROUP BY 1 ORDER BY 1`

---

### Exit & Abort Breakdown

*   **Position Exit Reason Breakdown**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT exit_reason, COUNT(*) FROM reversion_trade_reports WHERE status = 'completed' AND exit_reason IS NOT NULL AND exit_reason <> '' AND exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY exit_reason`
*   **Abort Reasons Breakdown**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT ioc_reason, COUNT(*) FROM reversion_trade_reports WHERE status = 'aborted' AND ioc_reason IS NOT NULL AND ioc_reason <> '' AND exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY ioc_reason`

---

### Financial & Win/Loss Breakdown Pie Charts

*   **Net Profit Breakdown by Symbol**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT normalized_symbol, SUM(net_profit) AS total_profit FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY normalized_symbol`
*   **Win/Loss Ratio (Completed Trades)**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT CASE WHEN net_profit > 0 THEN 'Win' ELSE 'Loss' END AS outcome, COUNT(*) AS count FROM reversion_trade_reports WHERE status = 'completed' AND exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY 1`
*   **Total Fees Paid by Symbol**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT normalized_symbol, SUM(fee) AS total_fee FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY normalized_symbol`
*   **Funding Fee by Symbol**
    *   **Type**: Pie Chart
    *   **Query**: `SELECT normalized_symbol, SUM(hold_fee) AS funding_fee FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) GROUP BY normalized_symbol`

---

### Historical Ledger

*   **Live Trade History Ledger Table**
    *   **Type**: Table
    *   **Query**: `SELECT timestamp, exchange, normalized_symbol, side, margin_usdt, leverage, fill_price, close_price, net_profit, hold_duration_ms, status, exit_reason, error_msg FROM reversion_trade_reports WHERE exchange IN ($exchange) AND normalized_symbol IN ($symbol) AND $__timeFilter(timestamp) ORDER BY timestamp DESC`
