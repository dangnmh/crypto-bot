# Wall Trust Score

> Status: heuristic seed. This document is the source of truth for wall scoring. The score is a probabilistic filter, not proof that a wall is real. Runtime lifecycle is in [flow.md](flow.md).

## Goal

Score each detected orderbook wall from `0` to `100` and only allow Penny Jumper workflows when the wall is likely enough to be real.

```text
trust_score = w1 * age_score
            + w2 * size_score
            + w3 * absorption_score
            + w4 * stability_score
            + w5 * context_score
            + w6 * historical_score
            + penalty
```

Recommended threshold: `trust_score >= 65`.

## Factor Weights

| Factor | Weight | Why |
|---|---:|---|
| `age_score` | 20% | Spoof walls often vanish within a few seconds |
| `size_score` | 15% | Wall must be materially larger than nearby liquidity |
| `absorption_score` | 25% | A wall that survives opposing flow is more credible |
| `stability_score` | 15% | Frequent resize/cancel behavior is suspicious |
| `context_score` | 15% | Wall location must make market-structure sense |
| `historical_score` | 10% | Repeated spoof behavior at same level should penalize |

## Age Score

Wall age is measured from first continuous detection at the same price level.

| Age | Score |
|---|---:|
| `< 1s` | 0 |
| `< 3s` | 20 |
| `< 10s` | 50 |
| `< 30s` | 75 |
| `< 60s` | 90 |
| `>= 60s` | 100 |

Cold start naturally scores low. Do not add a separate grace period unless paper-trading data proves it improves decisions.

## Size Score

Compare wall volume to average volume across nearby same-side orderbook levels.

```text
ratio = wall_volume / avg_volume_per_level
```

| Ratio | Score |
|---|---:|
| `< 5x` | 0 |
| `< 10x` | 30 |
| `< 20x` | 60 |
| `< 50x` | 85 |
| `< 100x` | 100 |
| `>= 100x` | 70 |

Very large walls are penalized because low-cap spoof walls can be deliberately oversized to attract retail flow.

## Absorption Score

Absorption estimates how much opposing pressure the wall has survived.

```text
absorption_ratio = absorbed_volume / original_wall_volume
```

| Absorption ratio | Score |
|---|---:|
| `< 0.01` | 10 |
| `< 0.05` | 40 |
| `< 0.15` | 70 |
| `< 0.30` | 90 |
| `>= 0.30` | 100 |

### MEXC Depth Limitation

`sub.depth.step0` is an aggregated depth snapshot stream. It does not prove individual market orders hit the wall. Absorption is inferred from wall volume changes across snapshots.

Only count inferred absorption when:

1. Wall volume declines gradually across at least three consecutive snapshots.
2. The wall remains at the same price level.
3. The decline is not an abrupt disappearance.
4. Stability score does not indicate aggressive owner-side resizing.

If these conditions are not met, keep absorption conservative.

## Stability Score

Count meaningful wall-volume resizes within the scoring window. A resize is meaningful when volume changes by more than 5%.

| Resize count in 30s | Score |
|---|---:|
| `0` | 100 |
| `1` | 70 |
| `2` | 40 |
| `>= 3` | 10 |

## Context Score

Context score can be negative. Clamp the final trust score to `[0, 100]`.

| Condition | Delta |
|---|---:|
| Wall sits at round number | `+30` |
| Wall near recent support/resistance | `+30` |
| Spread `< 0.3%` | `+20` |
| Wall is in the middle of no clear structure | `-20` |
| Bid and ask walls appear simultaneously around price | `-30` |
| 24h volume below configured soft floor | `-20` |

## Historical Score

Track wall events by `symbol + side + price_level` over a rolling 1-4 hour window.

| History | Score |
|---|---:|
| Same level had wall pulled at least 2 times in 1h | 0 |
| Same level had wall consumed/filled and price respected it | 80 |
| First observation | 50 |

Historical memory is local strategy state. It should be persisted only if restart behavior requires continuity.

## Penalties

| Situation | Penalty |
|---|---:|
| Large opposing trade burst appears with wall | `-20` |
| Wall volume exceeds 30% of same-side orderbook volume | `-15` |
| Symbol pumped more than 10% in 1h | `-25` |
| Wall distance from best bid/ask exceeds 1% | `-30` |
| Queue competition detected ahead of wall | configurable, seed `-20` |

## Example: Qualified Wall

```text
age_score        = 75
size_score       = 85
absorption_score = 70
stability_score  = 100
context_score    = 60
historical_score = 50
penalty          = 0

trust_score = 0.20*75 + 0.15*85 + 0.25*70 + 0.15*100 + 0.15*60 + 0.10*50
            = 74.25
```

Result: qualified when threshold is `65`.

## Example: Spoof-Like Wall

```text
age_score        = 20
size_score       = 70
absorption_score = 10
stability_score  = 40
context_score    = -20
historical_score = 0
penalty          = -15

trust_score = 0.20*20 + 0.15*70 + 0.25*10 + 0.15*40 + 0.15*(-20) + 0.10*0 - 15
            = 5.0
```

Result: skipped.

## Implementation Notes

| Requirement | Reason |
|---|---|
| Use realtime WebSocket data | REST polling is too slow for score freshness |
| Keep ring buffer of depth snapshots | Needed for age, absorption and stability |
| Keep rolling wall event history | Needed for historical spoof/consume behavior |
| Emit score breakdown | Required for tuning and audit |
| Clamp final score to `[0, 100]` | Prevent negative context/penalties from leaking odd values |

## Open Questions

| Question | Current stance |
|---|---|
| Should absorption require trade stream confirmation? | Prefer depth-only heuristic first; add trade stream only if false positives are high |
| Should weights be per-symbol liquidity bucket? | Likely yes after paper-trading data exists |
| Should score threshold be side-specific? | Keep one threshold until enough short/long samples exist |

## Backlog

| Priority | Item |
|---|---|
| P1 | Unit-test canonical true-wall and spoof-wall examples |
| P1 | Add score breakdown to workflow journal |
| P2 | Add queue-competition score factor |
| P2 | Add symbol/liquidity bucket calibration report |
