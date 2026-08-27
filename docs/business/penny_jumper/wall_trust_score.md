# Wall Trust Score & Local Model Judgment

> Status: Canonical specification for wall trust scoring and local model evaluation (`WallJudge`). Runtime lifecycle is documented in [09_wall_event_sourcing_and_storage_flow.md](09_wall_event_sourcing_and_storage_flow.md) and [03_wall_trust_scoring_flow.md](03_wall_trust_scoring_flow.md).

## Goal

Evaluate each detected orderbook wall dynamically from its point-in-time event stream (`[]WallEvent`) to determine whether it represents genuine structural liquidity (`IsTrusted = true`, `TrustScore >= 0.75`) or predatory spoofing.

---

## 1. Local Model & Scorer Architecture (`WallJudge`)

The evaluation engine decouples scoring logic from the orderbook ingestion pipeline:

```go
type WallJudgeResult struct {
    WallID     string  `json:"wall_id"`
    TrustScore float64 `json:"trust_score"`
    IsTrusted  bool    `json:"is_trusted"`
    Reason     string  `json:"reason"`
}

type WallJudge interface {
    JudgeWall(ctx context.Context, wall *Wall, events []WallEvent) (WallJudgeResult, error)
}
```

The `WallJudge` can be implemented via:
1. **Deterministic Rule-Based Evaluator**: Fast, zero-allocation heuristic using the weighted factors below.
2. **Local XGBoost / Classifier**: Tree-based model trained on labeled historical event sequences.
3. **Local Small Language Model (SLM)**: Quantitative reasoning model evaluating inter-arrival times and size patterns.

---

## 2. Factor Weights for Rule-Based Scoring

```text
trust_score = w1 * age_score
            + w2 * size_score
            + w3 * absorption_score
            + w4 * stability_score
            + w5 * context_score
            + w6 * historical_score
            + penalty
```

| Factor | Weight | Microstructure Rationale |
|---|---:|---|
| `age_score` | 20% | Spoof walls typically flash and vanish in $< 2\text{s}$ |
| `size_score` | 15% | Wall must stand out relative to nearby average depth |
| `absorption_score` | 25% | A wall that fills incoming taker flow (`WALL_ABSORBED`) proves real liquidity |
| `stability_score` | 15% | Frequent maker size modifications (`WALL_RESIZED`) indicate algorithmic bot flickering |
| `context_score` | 15% | Proximity to support/resistance, round numbers, and tight spread |
| `historical_score` | 10% | Serial pull history at the same price level in `DepthStore` |

---

## 3. Factor Breakdown

### 1. Age Score ($20\%$)
Measured from `WallEventBorn` or `wall.GetAgeAt(now)`:
* $< 1\text{s}$: **0**
* $1\text{s} - 3\text{s}$: **20**
* $3\text{s} - 10\text{s}$: **50**
* $10\text{s} - 30\text{s}$: **75**
* $30\text{s} - 60\text{s}$: **90**
* $\ge 60\text{s}$: **100**

### 2. Relative Size Score ($15\%$)
$$\text{RelativeRatio} = \frac{\text{WallVolume}}{\text{AvgNearbyVolume}}$$
* $< 5\times$: **0**
* $5\times - 10\times$: **30**
* $10\times - 20\times$: **60**
* $20\times - 50\times$: **85** (Optimal)
* $50\times - 100\times$: **100** (Prime structural wall)
* $\ge 100\times$: **70** (Penalized for oversized spoof risk)

### 3. Absorption Score ($25\%$)
Derived dynamically via trade tape reconciliation (`domain.ReconcileWallData`):
$$\text{Metrics} = \text{domain.ReconcileWallData(wall, events, trades)}$$
$$\text{AbsorptionRatio} = \frac{\text{Metrics.AbsorbedVolume}}{\text{InitialVolume}}$$
* $< 1\%$: **10** (Untested)
* $1\% - 5\%$: **40** (Initial fills confirmed)
* $5\% - 15\%$: **70** (Active absorption)
* $15\% - 30\%$: **90** (Heavy genuine absorption)
* $\ge 30\%$: **100** (Rock-solid institutional fill)

### 4. Stability Score ($15\%$)
Derived dynamically from the event stream:
$$\text{ResizeCount} = \text{domain.CalculateResizeCount(events)}$$
* $0$ resizes: **100**
* $1$ resize: **70**
* $2$ resizes: **40**
* $\ge 3$ resizes: **10** (High-frequency spoof flickering)

---

## 4. Real-Time Reactive Evaluation Triggers

The local model is invoked on specific lifecycle event stream transitions:
1. **On Maturation (`WALL_MATURED`)**: Initial entry evaluation.
2. **On Taker Absorption (`WALL_ABSORBED`)**: Boosts trust score when taker volume fills into the wall.
3. **On Maker Resize (`WALL_RESIZED`)**: Re-evaluates spoof probability when maker adds/modifies size.
4. **On Price Approach (`PRICE_APPROACHED`)**: Verifies the wall does not pull when price is 1 tick away.
5. **On Disappearance / Consumption (`WALL_DISAPPEARED` / `WALL_CONSUMED`)**: Produces ground-truth labels for offline ML model training.
