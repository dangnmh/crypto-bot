# Orderbook Usage

Status: implemented constraints and accepted assumptions.

Orderbook is useful for immediate execution decisions. It is weak as a future-price predictor around settlement.

## Accepted Uses

| Use | Status | Rule |
|---|---|---|
| IOC slippage estimation | Implemented | Sweep near-side OB at fire time |
| Spread-based slippage | Implemented | Use spread multiplier as dynamic buffer |
| TP wall safety cap | Implemented with caution | Wall can reduce TP target, not increase it |
| Trap OB assistance | Implemented with caution | Use as cap/placement aid, not primary model |
| Imbalance Ratio | Backlog | Filter only, not primary signal |
| Multi-snapshot wall protocol | Backlog | Needs persistence and spoofing guards |

## Core Principle

```text
FR decides direction.
FR + ATR decide target range.
Orderbook decides immediate execution safety.
Trailing stop handles exit.
```

## Why OB Around Settlement Is Weak

Before funding settlement, many traders and market makers remove or replace liquidity. A wall visible before or just after settlement can disappear before price reaches it.

| Purpose | Reliability | Reason |
|---|---|---|
| IOC slippage | High | Uses current liquidity for immediate order execution |
| TP wall prediction | Medium/low | Wall may disappear before TP is reached |
| Trap placement | Medium/low | Wick and liquidity reset are unstable after settlement |
| Imbalance signal | Low/medium | Spoofing is common on thin altcoins |

This is why OB should cap risk, not override the statistical FR/ATR model.

## Altcoin Constraints

| Constraint | Impact |
|---|---|
| Low 24h volume | Notional can create meaningful impact |
| Wide spread | Static slippage is often too naive |
| Thin levels | OB sweep is more useful than fixed percent |
| Spoofing | Wall size alone is not reliable |
| Fast settlement wick | Trap needs separate journal and fast exit |

## Implemented Logic

### IOC Slippage

For Reversion, OB sweep estimates the price required to fill the intended volume.

Required journal fields:

- `ioc_intended_price`
- `ioc_fill_price`
- `ioc_slippage_pct`
- `best_bid`
- `best_ask`
- `spread`
- `volume`
- `ref_price`

### TP Wall Safety Cap

TP should primarily come from FR/ATR dynamic pricing. If a wall is detected before the configured TP, TP may be capped before the wall.

Rules:

- A wall may reduce TP.
- A wall should not increase TP beyond FR/ATR range.
- A stale OB snapshot should fall back to config/dynamic TP.
- Journal must record whether wall cap was used.

### Trap OB Assistance

Trap should primarily use FR-derived depth. OB can adjust/cap placement only when wall quality is acceptable.

Required journal fields:

- `trap_source`
- `wall_price`
- `wall_distance_pct`
- `wall_verified`
- `trap_outcome`

## Backlog Moved Out

The following ideas are intentionally not specified here as implemented behavior:

- Imbalance Ratio.
- Dynamic TP/SL expansion based on OB.
- Multi-snapshot wall persistence protocol.
- Runtime wall verification.

See [backlog.md](backlog.md) for work items and [concern.md](concern.md) for risk.
