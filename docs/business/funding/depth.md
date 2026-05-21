# Orderbook Usage

Status: implemented constraints and accepted assumptions.

Orderbook is useful for immediate execution decisions. It is weak as a future-price predictor around settlement.

## Accepted Uses

| Use | Status | Rule |
|---|---|---|
| IOC slippage estimation | Removed from Reversion | Reversion uses static slippage only |
| Spread-based slippage | Removed from Reversion | No dynamic spread multiplier in Reversion |
| TP wall safety cap | Removed from Reversion | Wall does not change Reversion TP |
| Trap OB assistance | Implemented with caution | Use as cap/placement aid, not primary model |
| Imbalance Ratio | Removed from Reversion | Not used as a trading gate |
| Multi-snapshot wall protocol | Backlog | Needs persistence and spoofing guards |

## Core Principle

```text
FR decides direction.
Static config decides Reversion TP/SL.
Orderbook is not used by Reversion.
Trap may still use fresh wall verification.
```

## Why OB Around Settlement Is Weak

Before funding settlement, many traders and market makers remove or replace liquidity. A wall visible before or just after settlement can disappear before price reaches it.

| Purpose | Reliability | Reason |
|---|---|---|
| IOC slippage | Not used by Reversion | Static `maxPriceDiffPercent` is the current Reversion rule |
| TP wall prediction | Medium/low | Wall may disappear before TP is reached |
| Trap placement | Medium/low | Wick and liquidity reset are unstable after settlement |
| Imbalance signal | Low/medium | Spoofing is common on thin altcoins |

This is why OB should not override the static Reversion TP/SL contract.

## Altcoin Constraints

| Constraint | Impact |
|---|---|
| Low 24h volume | Notional can create meaningful impact |
| Wide spread | Tune static slippage conservatively or skip the symbol |
| Thin levels | Static slippage may be insufficient; journal fill quality by symbol |
| Spoofing | Wall size alone is not reliable |
| Fast settlement wick | Trap needs separate journal and fast exit |

## Implemented Logic

### IOC Slippage

Reversion no longer uses OB sweep for IOC slippage. It uses static `maxPriceDiffPercent` plus a minimum two-tick buffer.

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

Removed from Reversion. Walls around settlement are too unstable to decide Reversion TP. Static TP/SL is the current runtime contract.

### Trap OB Assistance

Trap should primarily use FR-derived depth. OB can adjust/cap placement only when wall quality is acceptable.

Runtime rule:

- OB-assisted Trap must verify the wall with a fresh orderbook immediately before placement.
- If the wall disappears or cannot be verified, skip the OB-assisted Trap.
- If a fresh valid wall moved, recalculate Trap price from the fresh wall.

Required journal fields:

- `trap_source`
- `wall_price`
- `wall_age_ms`
- `wall_distance_pct`
- `wall_verified`
- `trap.outcome`

### Imbalance Ratio

Not used by Reversion. The old optional imbalance filter was removed from the Reversion runtime because spoofing around settlement makes it weak as a trading gate.

The old ratio was:

```text
imbalance_ratio = bid_volume_near_price / ask_volume_near_price
```

## Backlog

The following ideas are intentionally not specified here as implemented behavior:

- Dynamic TP/SL expansion based on OB remains intentionally out of Reversion.
- Multi-snapshot wall persistence protocol.

## Open Questions And Concerns

| Item | Current stance |
|---|---|
| OB wall quanh settlement là unstable | Reversion ignores OB; Trap OB must verify a fresh wall before placement |
| Imbalance Ratio dễ bị spoof | Removed from Reversion; do not use it as an entry gate |
| Market snapshots may be stale | Add stale-market-data guards before acting on older OB/ticker snapshots |
| Multi-snapshot wall protocol | Future work; requires persistence and spoofing guards before walls become stronger signals |
