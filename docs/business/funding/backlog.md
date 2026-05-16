# Funding Backlog

Backlog chỉ chứa việc chưa làm hoặc chưa đủ dữ liệu để làm. Câu hỏi mở nằm ở [question.md](question.md), suggestion nằm ở [suggest.md](suggest.md), concern của logic hiện tại nằm ở [concern.md](concern.md).

## Priority Backlog

| Priority | Item | Why | Owner doc |
|---|---|---|---|
| P2 | Imbalance Ratio filter | Chỉ dùng filter phụ vì spoof-prone | [depth.md](depth.md) |
| P3 | Pre-Funding Wave implementation | Cần journal chứng minh edge trước | [pre_funding_flow.md](pre_funding_flow.md) |

## P0 Details

### Minimal Cycle Recorder JSONL

Status: implemented. JSONL records include schema identity, flow-scoped Reversion/Trap fields, legacy cycle excursion, and explicit `ioc_excursion` / `trap_excursion` so Trap tuning is not hidden by Reversion outcome.

Done criteria:

- Record `done`, `abort`, `timeout`, and `no_fill`.
- Include `schema_version`.
- Include `req_id`, `symbol`, `settle_time`, `flow`.
- Include Reversion fields when Reversion runs.
- Include Trap fields when Trap runs.
- Include MFE/MAE from fill until close/timeout.
- Recorder failure logs error but does not panic trading path.
- Unit tests cover record assembly and append failure.

### Split Event Topics By Flow

Status: implemented. Shared scan fans out to flow-specific candidate topics, and downstream Reversion/Trap order, fill, trailing, timeout, close, error, and abort events remain in their own namespaces. Trap terminal events no longer route through Reversion abort; cleanup listens to terminal topics for both flows.

Target topic layout:

```text
funding.scan.candidate_found
funding.reversion.candidate
funding.reversion.ioc_fired
funding.reversion.order_filled
funding.reversion.trailing_placed
funding.trap.candidate
funding.trap.order_placed
funding.trap.order_filled
funding.trap.trailing_placed
funding.prefunding.candidate
```

Shared scan can publish to multiple flow candidate topics. Downstream handlers should not consume one generic confirmation topic for unrelated flow behavior.

### Percent-Unit Audit

Status: implemented. Config normalization now preserves decimal ratio inputs for `*Pct` fields, accepts both percent and decimal funding thresholds, keeps `maxPriceDiffPercent` in percent units for slippage math, and journal percent outputs remain percent values.

Audit config, docs, code, and journal fields:

| Field type | User-facing | Internal |
|---|---:|---:|
| TP/SL/depth/trailing percent | `3` | `0.03` |
| Funding threshold | `0.3` or `0.003` | `0.003` |
| Slippage percent | `0.8` | `0.8` percent, not ratio |
| Journal percent output | `3.0` | avoid mixed schemas |

## P1 Details

Status: P1 implementation complete. Trap sizing, cycle exposure caps, and JSONL daily report/query are implemented. Keep this section as implementation notes and tuning rationale.

### Trap Size Ratio

Status: implemented in config/order sizing. Loaded configs default `fundingTrap.sizeRatio = 0.5`; optional `fundingTrap.maxNotionalUSDT` caps Trap notional before volume conversion.

Proposed config:

```jsonc
{
  "fundingTrap": {
    "sizeRatio": 0.5,
    "maxNotionalUSDT": 10000
  }
}
```

Start with 25%-50% of Reversion notional until journal proves stable expectancy.

### Cycle Exposure Cap

Status: implemented as pre-order risk guard for Reversion and Trap. The guard checks configured notional and estimated SL loss for the combined `symbol + settle_time` cycle before adding a leg.

Proposed config:

```jsonc
{
  "safety": {
    "maxCycleNotionalUSDT": 30000,
    "maxCycleLossUSDT": 150,
    "disableSymbolAfterCriticalCloseFailure": true
  }
}
```

The risk controller should see all flow legs for the same `symbol + settle_time`.

### Journal Daily Report/Query

Status: implemented as `cmd/funding-journal`.

Example:

```bash
go run ./cmd/funding-journal -dir data/journal -date 2026-05-15 -symbol BTC_USDT
go run ./cmd/funding-journal -dir data/journal -date 2026-05-15 -json
```

## P2 Details

### Exact-Leg Fallback Close API

Status: implemented. TrackOrder fallback now tries exact-leg close first, using the filled event's `close_side`, `deal_vol`, and configured `positionMode`. If exact close fails, `CloseAllPositions(symbol)` remains the final last-resort safety path.

Current safety path:

- If TrackOrder placement fails after a fill, fallback close uses `ClosePosition(symbol, closeSide, volume, positionMode)` with a fresh uncancelled timeout context.
- If exact close fails, fallback close uses `CloseAllPositions(symbol)` with the same fresh uncancelled timeout context.
- If fallback close succeeds, cycle exits with `trailing_failed_fallback`.
- If fallback close fails, cycle journal records `critical_close_failed`, emits a flow error, aborts, and does not publish a false `position_closed`.
- If post-settle timeout force-close fails, cycle journal records `critical_timeout_close_failed`, emits a flow error, aborts, and does not publish a false timeout.

### Runtime Trap Wall Verification

Status: implemented for OB-assisted Trap.

If using OB-assisted Trap:

- The Trap flow reads a fresh orderbook immediately before OB Trap placement.
- If the wall disappears or cannot be verified, the OB Trap is skipped instead of placing from a stale wall.
- If the wall moved but another valid wall exists, the Trap price is recalculated from the fresh wall.
- Journal records `wall_verified`, `wall_age_ms`, `wall_distance_pct`, and `wall_price`.

### Trap Order Timeout / Cancel

Status: implemented for unfilled Trap orders.

Current safety path:

- After `funding.trap.order_placed`, the Trap flow starts an independent timeout from `fundingTrap.postSettleTimeout` with a 60s fallback.
- If the Trap order is still unfilled when the timer expires, bot calls `CancelOrder(symbol, orderID)`.
- If single-order cancel fails, bot falls back to `CancelAllOpenOrders(symbol)`.
- If open-order cancellation fails, journal records `critical_trap_cancel_failed`, emits a Trap error, and aborts the cycle instead of leaving the stale limit order invisible.

### Imbalance Ratio Filter

Do not use as primary signal. If implemented, use as filter or journal feature:

```text
imbalance_ratio = bid_volume_near_price / ask_volume_near_price
```

Guardrails:

- Use near levels only.
- Require persistence if acting on walls.
- Compare by symbol/liquidity bucket.

## P3 Details

### Pre-Funding Wave

Implement only after journal proves:

- pre-settlement movement exists by FR bucket,
- confirmation avoids late entries,
- force-close prevents Reversion conflict,
- funding transfer impact is known if holding through settle is considered.
