# Penny Jumper Strategy Analysis

> Status: strategy analysis. This document describes the trading thesis, market assumptions, risk model and operating constraints. Runtime lifecycle lives in [flow.md](flow.md); scoring logic lives in [wall_trust_score.md](wall_trust_score.md).

## Goal

Penny Jumper targets altcoin, low-cap and thin-liquidity futures where one visible orderbook wall can temporarily anchor price. The bot places a post-only maker order one tick in front of a qualified wall, then exits quickly through maker TP, trailing stop, timeout or emergency market bailout.

The edge is not microsecond co-location. The edge is selecting venues and symbols where:

1. Tier-1 HFT competition is limited because size capacity is too small.
2. Tick value is wide enough that one or a few ticks can cover fees and bailout risk.
3. Retail/manual flow is slow enough that 50-100ms reaction can still matter.
4. Some large walls are genuine liquidity rather than spoofing.

## Target Market

| Market feature | Desired condition | Why |
|---|---|---|
| Symbol class | Altcoin/low-cap futures | Less institutional HFT pressure |
| 24h volume | Above configured floor | Avoid dead coins and impossible exits |
| Spread | Prefer `< 0.3%`; skip hard above configured max | Bailout cost must not erase edge |
| Wall distance | `<= 1%` from best bid/ask | Far walls do not protect entry |
| Tick size | Meaningful relative tick value | Maker jump needs enough gross edge |
| Funding rate | Checked before shorting | Negative funding can erase short-side PnL |

## Core Thesis

Use a visible wall as a short-lived support/resistance anchor:

| Wall | Direction | Entry | Primary exit | Emergency exit |
|---|---|---|---|---|
| Bid wall | Long | Bid at `wall_price + 1 tick` | Maker TP above entry | Market sell if wall disappears |
| Ask wall | Short | Ask at `wall_price - 1 tick` | Maker TP below entry | Market buy if wall disappears |

The wall is not treated as permanent support. It is a condition that must remain true. If it is pulled, consumed too quickly, migrates unsafely, or the order does not fill before timeout, the workflow exits.

## Risk Model

| Risk | Failure mode | Required guard |
|---|---|---|
| Spoofing | Wall appears, attracts jumps, then disappears | [Wall Trust Score](wall_trust_score.md), age/stability/absorption/history penalties |
| Wide spread | Market bailout loses more than average TP | Spread guard and worst-case bailout pre-check |
| Partial fill | Small fill cannot amortize exit costs | Exit immediately if fill ratio below threshold |
| Queue competition | Many bots stack ahead of wall and cancel together | Detect clustered small orders and skip |
| Wall migration | Original wall disappears and reappears nearby | Treat only bounded same-size migration as adjust; otherwise bailout/cancel |
| Halt/delist | Position becomes trapped | Symbol warning blacklist and periodic contract metadata refresh |
| Rate limit/API ban | Too many place/cancel actions | Trust-score gate, max workflows, later token bucket |

## Operating Rules

| Rule | Default seed | Reason |
|---|---:|---|
| Wall size threshold | `>= 20x` average nearby level size | Detect meaningful imbalance |
| Wall distance max | `<= 1%` | Keep protection relevant |
| Trust score threshold | `>= 65` | Filter weak/spoof walls |
| Pending order timeout | `60s` | Avoid stale capital lock |
| Post-fill TP timeout | `120s` | Avoid drifting into unrelated market regime |
| Wall weak threshold | Volume down `> 50%` | Original premise is degraded |
| Trailing activation | Profit `> 0.3%` | Protect unusually strong runs |
| Partial fill minimum | `>= 30%` configured size | Avoid tiny uneconomic fills |

These values are heuristic seeds. They must be tuned from paper-trading records before production sizing.

## Position Sizing

Sizing is trust-score weighted and bounded by portfolio risk:

```text
max_position_per_trade = total_capital * 0.02
actual_size = max_position_per_trade * (trust_score / 100)
```

Example:

| Input | Value |
|---|---:|
| Total capital | `$2,000` |
| Max per trade | `$40` |
| Trust score | `80` |
| Actual margin | `$32` |

Additional guards:

| Guard | Rule |
|---|---|
| Concurrent positions | Max 3-5 open positions |
| Symbol concentration | Max 1 active workflow per symbol |
| Daily loss limit | Stop bot if daily realized loss exceeds 5% capital |
| Correlated market dump | Prefer fewer simultaneous low-cap positions during broad market volatility |

## Fee And PnL Calculator

Seed fee assumptions for MEXC Futures:

```text
maker_fee = 0.01%
taker_fee = 0.05%

gross_pnl_pct = (exit_price - entry_price) / entry_price * 100
net_pnl_pct = gross_pnl_pct - entry_fee_pct - exit_fee_pct
```

| Scenario | Entry | Exit | Gross | Fees | Net PnL |
|---|---|---|---:|---:|---:|
| TP maker | Maker | Maker | `+0.60%` | `0.02%` | `+0.58%` |
| Strong TP maker | Maker | Maker | `+1.40%` | `0.02%` | `+1.38%` |
| Bailout narrow | Maker | Taker | `-0.60%` | `0.06%` | `-0.66%` |
| Bailout wide | Maker | Taker | `-1.00%` | `0.06%` | `-1.06%` |

Breakeven depends heavily on bailout cost. With average TP `+0.58%` and average loss `-0.66%`, required win rate is about 53%. If bailout widens to `-1.06%`, required win rate rises to about 65%.

## Known Concerns

| Concern | Current stance |
|---|---|
| Strategy can become negative EV if spread widens | Spread and bailout-cost checks are mandatory before entry |
| `depth.step0` snapshots cannot prove true absorption | Absorption is a heuristic and must be labeled as such |
| Retail bot competition can erase queue priority | Queue competition detector is part of eligibility, not optional polish |
| Low-cap symbols can halt/delist suddenly | Contract/warning blacklist must run before live trading |

## Backlog

| Priority | Item |
|---|---|
| P1 | Define paper-trading journal schema for wall detection, score, order decision and terminal state |
| P1 | Add symbol blacklist source for delist/warning/halt states |
| P2 | Add queue-competition metric from orderbook levels around the wall |
| P2 | Tune trust threshold and TP/timeout buckets by symbol liquidity class |
