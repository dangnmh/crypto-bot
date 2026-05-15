# Journal Analysis Playbook

This document turns Cycle Recorder data into concrete config decisions. Use it after reading [analyze.md](analyze.md), which defines what the recorder should persist.

## Purpose

The journal is not only an audit log. It is the feedback loop for:

- IOC timing and slippage quality.
- TP/SL/trailing calibration.
- Trap depth and size decisions.
- Whether dynamic pricing beats static config.
- Whether a new strategy such as Pre-Funding deserves implementation.

Do not increase trading size or add strategy branches based on anecdotal cycle outcomes. Require journal evidence first.

## Current JSONL Compatibility

Existing records such as `data/journal/cycles-YYYY-MM-DD.jsonl` already resemble the target shape:

```json
{
  "req_id": "...",
  "symbol": "COS_USDT",
  "outcome": "aborted",
  "decision": {"fr_at_scan": -0.012579, "fr_at_recheck": -0.012082},
  "ioc": {"intended_price": 0.001726, "filled": false, "settle_offset_ms": -362},
  "trap": {"enabled": true, "filled": false},
  "exit": {"reason": "abort", "dynamic_pricing_enabled": true},
  "excursion": {},
  "config": {},
  "timeline": []
}
```

The current format is useful, but analysis must guard against mixed units. Some fields may be decimals (`0.03` = 3%) while dynamic fields may already be percent values (`2.5` = 2.5%). Until the schema is versioned and normalized, every report should run a unit sanity check.

## Unit Sanity Check

Before tuning, flag suspicious rows:

| Check | Suspicious condition | Likely problem |
|---|---|---|
| Config TP/SL | `tp_pct_configured < 0.2` | Stored as decimal, not percent |
| Dynamic TP/SL | `dynamic_tp_pct > 0.2` and `tp_pct_configured < 0.2` | Mixed percent/decimal fields |
| Trap config | `trap.depthPct < 0.2` | Config snapshot likely stores decimal |
| Funding rate | `abs(fr_at_scan) > 0.2` | Funding rate probably parsed incorrectly |
| Slippage | `ioc_slippage_pct > 20` | Either bad fill or unit bug |
| Empty excursion | `outcome != "aborted"` and missing `mfe_pct`/`mae_pct` | MFE/MAE sampler missing or failed |

Recommended normalization for reports:

```text
normalized_percent =
  if abs(value) <= 0.2 then value * 100
  else value
```

Use this only in analysis scripts. Production schema should instead define units explicitly and avoid heuristic conversion.

## Daily Review

Run this review after each funding session:

| Question | Metric | Action |
|---|---|---|
| Did IOC fire at the intended time? | `settle_offset_ms` distribution | Adjust latency buffer if consistently early/late |
| Did IOC fail before fill? | `outcome=aborted`, `abort_phase=fire_ioc`, `ioc.error` | Fix order price/TP/SL validation before tuning strategy |
| Did slippage exceed model? | `ioc_slippage_pct` | Increase OB buffer or reduce size if systematic |
| Did trailing capture the move? | `tp_efficiency = actual_exit_pct / mfe_pct` | Tune activation/callback if efficiency is low |
| Was TP reachable? | `mfe_vs_tp` | Lower TP if MFE is usually below TP |
| Was SL too tight? | `mae_vs_sl`, `sl_was_touched` | Widen SL if winners repeatedly exceed configured SL drawdown |
| Did Trap add value? | `trap_outcome`, `trap_mfe_pct`, `trap_mae_pct` | Reduce/disable Trap if it adds drawdown without positive expectancy |

## Reversion Tuning Rules

Use at least 30 comparable cycles per symbol or FR bucket before changing config.

| Observation | Interpretation | Config response |
|---|---|---|
| Median `settle_offset_ms` < -150ms | Orders arrive too early | Reduce buffer or fire later |
| Median `settle_offset_ms` > 150ms | Orders arrive late | Increase fire offset or latency estimate |
| `ioc_slippage_pct` high while fill rate low | Price too conservative for IOC | Increase slippage cap/buffer or reduce notional |
| `ioc_slippage_pct` high while fill rate high | Market impact too large | Reduce notional or avoid thin symbols |
| `mfe_pct` consistently below configured TP | TP too ambitious | Lower TP multiplier or trailing activation |
| `mfe_pct` far above exit capture | Exit too conservative | Use trailing as primary or widen callback |
| `mae_pct` often exceeds SL on profitable cycles | SL too tight | Widen SL or use ATR component |
| `mae_pct` low across losing cycles | SL too loose | Tighten SL or add faster failure exit |

## Trap Tuning Rules

Analyze Trap separately from Reversion. A profitable Reversion should not hide an unprofitable Trap leg.

| Observation | Interpretation | Config response |
|---|---|---|
| `trap_filled=false` for most cycles | Trap depth too far or wick absent | Reduce depth multiplier or disable for that FR bucket |
| `trap_filled=true` but `trap_mae_pct` high | Catching continuation, not bounce | Increase depth, reduce size, or require wall verification |
| `trap_mfe_pct` below TP | Trap TP too ambitious | Lower Trap TP or rely on immediate trailing |
| OB-assisted worse than static | OB wall path is not adding value | Use FR-static path as primary and demote OB to cap only |
| Trap losses cluster by symbol | Symbol-specific liquidity issue | Disable Trap for that symbol |
| Trap outcome volatile with same FR bucket | Position size too large for edge certainty | Lower `trapSizeRatio` |

Initial sizing rule: keep Trap notional at 25%-50% of Reversion until the journal shows stable positive expectancy.

## Dynamic Pricing Review

Dynamic pricing must beat static baselines in comparable market conditions. Record both dynamic and static values.

| Metric | Good sign | Bad sign |
|---|---|---|
| `dynamic_tp_pct` vs `mfe_pct` | Dynamic TP tracks reachable MFE | Dynamic TP frequently above MFE |
| `dynamic_sl_pct` vs `mae_pct` | SL survives normal noise | SL would stop many winners |
| ATR contribution | Wider only on volatile symbols | Widens targets without higher MFE |
| FR multiplier contribution | Higher FR maps to larger realized move | Higher FR does not improve MFE/outcome |

If dynamic pricing does not outperform static after enough cycles, freeze dynamic config and tune one multiplier at a time.

## Abort Analysis

Abort cycles are valuable. Do not filter them out.

| Abort phase | Likely issue | Next action |
|---|---|---|
| `scan` | FR threshold or symbol filtering | Review opportunity set |
| `arm` | WS/market data/volume safety | Check subscriptions and contract metadata |
| `recheck` | FR instability | Keep filter; it may be saving bad trades |
| `fire_ioc` | Exchange order validation | Inspect intended price, TP, SL, tick/scale snapping |
| `fill_watcher` | IOC did not fill | Check slippage, timing, and liquidity |
| `trailing` | TrackOrder failure | Verify fallback close and journal `trailing_failed_fallback` |

Example from current journal data: `abort_phase=fire_ioc` with MEXC error `The price of stop-limit order error` should be treated as an order-construction bug or exchange constraint mismatch before any strategy tuning.

## Report Outputs

Current CLI:

```bash
go run ./cmd/funding-journal -dir data/journal -date YYYY-MM-DD
go run ./cmd/funding-journal -dir data/journal -date YYYY-MM-DD -symbol BTC_USDT -json
```

A useful daily report should include:

| Section | Minimum content |
|---|---|
| Summary | cycles, traded, aborted, no-fill, closed |
| Timing | avg/median/min/max `settle_offset_ms` |
| Execution | IOC fill rate, avg slippage, order errors |
| Reversion | MFE/MAE, exit reason distribution, TP efficiency |
| Trap | fill rate, source comparison, Trap PnL/excursion from `trap.excursion` |
| Config recommendations | exact config fields to change, with evidence |

Every recommendation should name the evidence window, for example: "last 42 COS_USDT cycles" or "FR bucket 0.6%-1.2% across 18 cycles".
