# Journal Analysis Playbook

This document turns Cycle Recorder data into concrete config decisions. Use it after reading [analyze.md](analyze.md), which defines what the recorder should persist.

## Purpose

The journal is not only an audit log. It is the feedback loop for:

- IOC timing and slippage quality.
- Static TP/SL calibration.
- Trap depth and size decisions.
- Whether a new strategy such as Pre-Funding deserves implementation.

Do not increase trading size or add strategy branches based on anecdotal cycle outcomes. Require journal evidence first.

## Current JSONL Compatibility

Existing records such as `data/journal/cycles-YYYY-MM-DD.jsonl` already resemble the target shape:

```json
{
  "schema_version": 2,
  "req_id": "...",
  "symbol": "COS_USDT",
  "flows": ["reversion", "trap"],
  "outcome": "aborted",
  "decision": {"fr_at_scan": -0.012579, "fr_at_recheck": -0.012082},
  "ioc": {"flow": "reversion", "intended_price": 0.001726, "filled": false, "settle_offset_ms": -362},
  "trap": {"flow": "trap", "enabled": true, "filled": false},
  "exit": {"reason": "abort"},
  "ioc_excursion": {},
  "trap_excursion": {},
  "config": {},
  "timeline": []
}
```

The current format is useful, but analysis must guard against older mixed-unit records. Current config normalization preserves decimal ratio inputs for `*Pct` fields, accepts both percent and decimal funding thresholds, keeps `maxPriceDiffPercent` in percent units for slippage math, and journal percent outputs remain percent values. Reports should still run a unit sanity check for old JSONL data.

## Unit Sanity Check

Before tuning, flag suspicious rows:

| Check | Suspicious condition | Likely problem |
|---|---|---|
| Config TP/SL | `tp_pct_configured < 0.2` | Stored as decimal, not percent |
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
| Did IOC fail before fill? | `outcome=aborted`, `abort_topic=funding.reversion.abort`, `ioc.error`, timeline topic before abort | Fix order price/TP/SL validation before tuning strategy |
| Did slippage exceed model? | `ioc_slippage_pct` | Increase static `maxPriceDiffPercent` or reduce size if systematic |
| Was TP reachable? | `mfe_vs_tp` | Lower TP if MFE is usually below TP |
| Was SL too tight? | `mae_vs_sl`, `sl_was_touched` | Widen SL if winners repeatedly exceed configured SL drawdown |
| Did Trap add value? | `trap.outcome`, `trap.excursion.mfe_pct`, `trap.excursion.mae_pct` | Reduce/disable Trap if it adds drawdown without positive expectancy |

## Reversion Tuning Rules

Use at least 30 comparable cycles per symbol or FR bucket before changing config.

| Observation | Interpretation | Config response |
|---|---|---|
| Median `settle_offset_ms` < -150ms | Orders arrive too early | Reduce buffer or fire later |
| Median `settle_offset_ms` > 150ms | Orders arrive late | Increase fire offset or latency estimate |
| `ioc_slippage_pct` high while fill rate low | Price too conservative for IOC | Increase slippage cap/buffer or reduce notional |
| `ioc_slippage_pct` high while fill rate high | Market impact too large | Reduce notional or avoid thin symbols |
| `mfe_pct` consistently below configured TP | TP too ambitious | Lower static TP |
| `mfe_pct` far above exit capture | Exit too conservative | Raise static TP for that symbol/FR bucket |
| `mae_pct` often exceeds SL on profitable cycles | SL too tight | Widen static SL |
| `mae_pct` low across losing cycles | SL too loose | Tighten SL or add faster failure exit |

## Trap Tuning Rules

Analyze Trap separately from Reversion. A profitable Reversion should not hide an unprofitable Trap leg.

Before tuning depth or size, first verify the Trap terminal journal contract:

| Check | Bad sign | Action |
|---|---|---|
| Every Trap-enabled cycle has a terminal Trap result | Trap enabled but no closed/timeout/abort/skipped outcome | Fix journal schema/report before tuning |
| Skip reasons are counted | Many empty Trap records with no reason | Check `trap.skip_reason` and `funding.trap.skipped` |
| Filled Trap has MFE/MAE | `trap.filled=true` with empty `trap.excursion` | Fix Trap excursion sampler |
| Trap terminal state is independent from Reversion PnL | Profitable cycle hides Trap loss/abort/skip | Report Trap leg separately |

| Observation | Interpretation | Config response |
|---|---|---|
| `trap.filled=false` for most cycles | Trap depth too far or wick absent | Reduce depth multiplier or disable for that FR bucket |
| `trap.filled=true` but `trap.excursion.mae_pct` high | Catching continuation, not bounce | Increase depth, reduce size, or require wall verification |
| `trap.excursion.mfe_pct` below TP | Trap TP too ambitious | Lower Trap TP or rely on immediate trailing |
| OB-assisted worse than static | OB wall path is not adding value | Use FR-static path as primary and demote OB to cap only |
| Trap losses cluster by symbol | Symbol-specific liquidity issue | Disable Trap for that symbol |
| Trap outcome volatile with same FR bucket | Position size too large for edge certainty | Lower `trapSizeRatio` |

Initial sizing rule: keep Trap notional at 25%-50% of Reversion until the journal shows stable positive expectancy.

## Abort Analysis

Abort cycles are valuable. Do not filter them out.

| Abort evidence | Likely issue | Next action |
|---|---|---|
| `abort_topic=funding.scan.abort` | FR threshold or symbol filtering | Review opportunity set |
| abort after `funding.reversion.candidate` | WS/market data/volume safety | Check subscriptions and contract metadata |
| abort after `funding.reversion.wait_complete` | FR instability | Keep filter; it may be saving bad trades |
| abort after `funding.reversion.confirmed` | Exchange order validation | Inspect intended price, TP, SL, tick/scale snapping |
| no `funding.reversion.order_filled` after IOC | IOC did not fill | Check slippage, timing, and liquidity |
| `error_topic=funding.reversion.error` after fill | Timeout fallback close failure | Verify exact-leg close and fallback close-all behavior |

Example from current journal data: `abort_topic=funding.reversion.abort` after `funding.reversion.confirmed` with MEXC error `The price of stop-limit order error` should be treated as an order-construction bug or exchange constraint mismatch before any strategy tuning.

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
| Trap | enabled count, fill rate, source comparison, terminal outcome, skip reason, excursion from `trap.excursion` |
| FR buckets | same IOC/Trap metrics grouped by `abs(fr_at_scan)` bucket |
| Config recommendations | exact config fields to change, with evidence |

Every recommendation should name the evidence window, for example: "last 42 COS_USDT cycles" or "FR bucket 0.6%-1.2% across 18 cycles".

## Operating Rules

| Rule | Reason |
|---|---|
| Do not increase size from one or two good cycles | Avoid anecdotal tuning |
| Keep Trap notional at 25%-50% of Reversion until stable positive expectancy | Trap is higher risk and less proven |
| Compare by symbol and FR bucket | Global averages can hide thin-symbol behavior |
| Report Trap terminal outcomes even when Reversion is profitable | Cycle-level PnL can hide skipped, aborted, or losing Trap branches |
| Treat aborts as evidence | They expose validation, timing, liquidity, and FR instability problems |
