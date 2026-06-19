# Design Spec: P&L Analytics Dashboard Layout Reordering and Pie Chart Color Enhancements

## Goal
Improve the visual presentation of the Grafana `pnl-analytics` dashboard by reordering and resizing panels into a functional streamlined grid (Approach A) to eliminate gaps caused by previous deletions, and applying categorical color overrides to all remaining pie charts for consistent and professional look.

## Proposed Changes

### 1. Grid Position Reordering
We will update `gridPos` (`x`, `y`, `w`, `h`) for all 23 panels in [pnl-analytics.json](file:///home/four/projects/crypto-bot/deploy/grafana/dashboards/pnl-analytics.json) to form a cohesive, gapless grid of 24 units width:

| ID | Title | Row Group | x | y | w | h |
|---|---|---|---|---|---|---|
| 1 | Total Net Profit (USD) | KPI Stats | 0 | 0 | 4 | 4 |
| 2 | Win Rate (%) | KPI Stats | 4 | 0 | 4 | 4 |
| 3 | Completed Trades | KPI Stats | 8 | 0 | 4 | 4 |
| 4 | Total Volume (USDT) | KPI Stats | 12 | 0 | 4 | 4 |
| 5 | Funding Fee | KPI Stats | 16 | 0 | 4 | 4 |
| 6 | Total Fees Paid | KPI Stats | 20 | 0 | 4 | 4 |
| 7 | Hourly/Daily P&L Trend | P&L Trends | 0 | 4 | 12 | 8 |
| 8 | Equity Growth Curve | P&L Trends | 12 | 4 | 12 | 8 |
| 9 | Execution Latency (RTT) | Execution Diagnostics | 0 | 12 | 8 | 8 |
| 10 | Average Actual Slippage (%) | Execution Diagnostics | 8 | 12 | 8 | 8 |
| 11 | Target Buffer vs. Actual Fire Offset | Execution Diagnostics | 16 | 12 | 8 | 8 |
| 12 | Funding Fee Avoidance vs. Fire Offset | Funding Avoidance & Hold | 0 | 20 | 12 | 8 |
| 16 | Trade Hold Duration | Funding Avoidance & Hold | 12 | 20 | 12 | 8 |
| 14 | IOC Order Fill Ratio | IOC Metrics | 0 | 28 | 8 | 8 |
| 15 | IOC Cancel Rate (%) | IOC Metrics | 8 | 28 | 8 | 8 |
| 27 | IOC Cancel Rate by Exchange | IOC Metrics | 16 | 28 | 8 | 8 |
| 23 | Net Profit Breakdown by Exchange | Breakdown by Exchange | 0 | 36 | 8 | 8 |
| 24 | Total Fees Paid by Exchange | Breakdown by Exchange | 8 | 36 | 8 | 8 |
| 25 | Funding Fee by Exchange | Breakdown by Exchange | 16 | 36 | 8 | 8 |
| 26 | Win/Loss Ratio (Completed Trades) | Exit & Outcome Breakdown | 0 | 44 | 8 | 8 |
| 17 | Position Exit Reason Breakdown | Exit & Outcome Breakdown | 8 | 44 | 8 | 8 |
| 18 | Abort Reasons Breakdown | Exit & Outcome Breakdown | 16 | 44 | 8 | 8 |
| 19 | Live Trade History Ledger Table | Historical Ledger | 0 | 52 | 24 | 10 |

### 2. Pie Chart Color Overrides
We will add `fieldConfig.overrides` configuration to the following panels in `pnl-analytics.json` using exact string match overrides:

*   **Win/Loss Ratio (Completed Trades) (ID: 26)**
    *   `Win` $\rightarrow$ `green`
    *   `Loss` $\rightarrow$ `red`
*   **IOC Order Fill Ratio (ID: 14)**
    *   `filled` $\rightarrow$ `green`
    *   `partial_filled` $\rightarrow$ `yellow`
    *   `canceled_no_fill` $\rightarrow$ `blue`
    *   `unknown` $\rightarrow$ `red`
*   **Position Exit Reason Breakdown (ID: 17)**
    *   `target` $\rightarrow$ `green`
    *   `stop_loss` $\rightarrow$ `red`
    *   `timeout` $\rightarrow$ `orange`
    *   `force_close` $\rightarrow$ `dark-red`
*   **Abort Reasons Breakdown (ID: 18)**
    *   `ioc_canceled_no_position` $\rightarrow$ `blue`
    *   `ioc_outcome_unknown_no_position` $\rightarrow$ `purple`

### 3. Verification Plan
*   **Syntax Check**: Parse the modified `pnl-analytics.json` to verify it remains valid JSON.
*   **Overlap Check**: Programmatically check that no two panels have overlapping grid boundaries and that every panel fits within the 24-width constraint.
*   **Deployment**: Inform the user to run `make tf-apply-infra` to deploy the new dashboard config.
