# Trap Depth Calibration Guide

Status: heuristic seed, not production truth.

Use this file only as an initial calibration reference for `fundingTrap.depthPct` and FR-derived depth multipliers. Final config must come from journal data, especially `trap_filled`, `trap_mfe_pct`, `trap_mae_pct`, and `trap_outcome`.

## Cheat Sheet

| `abs(FR)` bucket | Initial trap depth | Expected fill | Note |
|---|---:|---|---|
| `< 0.3%` | skip | n/a | Funding edge likely too small |
| `0.3% - 0.6%` | `1.5% - 2.5%` | high | Use tighter depth; wick is often short |
| `0.6% - 1.2%` | `2.5% - 4.0%` | medium/high | Best initial research bucket |
| `1.2% - 2.0%` | `4.0% - 6.0%` | medium/low | Requires tighter risk control |
| `> 2.0%` | `6.0% - 10.0%+` | symbol-dependent | High volatility and high failure risk |

Rule of thumb:

```text
trapDepthPct ~= abs(FR%) * 3 to abs(FR%) * 5
```

Example: `abs(FR) = 0.8%`, multiplier `4.0` gives `3.2%` trap depth.

## Dynamic Trap Formula

```text
trapDepthPct = clamp(abs(FR%) * depthMultiplier, minDepth, maxDepth)
trapTPPct    = clamp(abs(FR%) * tpMultiplier, minTP, maxTP)
trapSLPct    = clamp(abs(FR%) * slMultiplier, minSL, maxSL)
```

Config percent values are user-facing: `depthPct: 2.5` means 2.5%.

## Calibration Metrics

| Metric | Interpretation |
|---|---|
| `trap_filled=false` often | Depth may be too far or wick absent |
| `trap_mae_pct` high after fill | Catching continuation, not bounce |
| `trap_mfe_pct` below TP | TP is too ambitious |
| `trap_source=ob_monitor` worse than `static_limit` | OB path should be demoted or disabled |
| losses clustered by symbol | Disable or reduce trap for that symbol |

## Guardrail

Do not increase Trap size from this guide alone. Require journal proof by symbol and FR bucket. See [journal_analysis.md](journal_analysis.md) and [concern.md](concern.md).
