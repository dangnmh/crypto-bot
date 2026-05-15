# Funding Backlog

Backlog chỉ chứa việc chưa làm hoặc chưa đủ dữ liệu để làm. Câu hỏi mở nằm ở [question.md](question.md), suggestion nằm ở [suggest.md](suggest.md), concern của logic hiện tại nằm ở [concern.md](concern.md).

## Priority Backlog

| Priority | Item | Why | Owner doc |
|---|---|---|---|
| P0 | Minimal Cycle Recorder JSONL with MFE/MAE | Không có dữ liệu thì TP/SL/Trap tuning là cảm tính | [analyze.md](analyze.md) |
| P0 | Split funding event topics by flow | Reversion/Trap/Pre-Funding chỉ nên share init scan | [flow.md](flow.md) |
| P0 | Percent-unit audit | Tránh nhầm `3` với `0.03` | [README.md](README.md) |
| P1 | Trap size ratio | Trap rủi ro hơn Reversion và không nên mặc định cùng size | [trap_flow.md](trap_flow.md) |
| P1 | Cycle exposure cap | Giới hạn tổng notional/loss của nhiều flow cùng symbol | [flow.md](flow.md) |
| P1 | Journal daily report/query | Biến JSONL thành quyết định config | [journal_analysis.md](journal_analysis.md) |
| P2 | Runtime trap wall verification | Giảm rủi ro paper wall trước/cùng lúc đặt Trap | [trap_flow.md](trap_flow.md) |
| P2 | Imbalance Ratio filter | Chỉ dùng filter phụ vì spoof-prone | [depth.md](depth.md) |
| P3 | Pre-Funding Wave implementation | Cần journal chứng minh edge trước | [pre_funding_flow.md](pre_funding_flow.md) |

## P0 Details

### Minimal Cycle Recorder JSONL

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

Audit config, docs, code, and journal fields:

| Field type | User-facing | Internal |
|---|---:|---:|
| TP/SL/depth/trailing percent | `3` | `0.03` |
| Funding threshold | Prefer `0.003 (0.3%)` | `0.003` |
| Journal percent output | `3.0` | avoid mixed schemas |

## P1 Details

### Trap Size Ratio

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

Proposed config:

```jsonc
{
  "risk": {
    "maxCycleNotionalUSDT": 30000,
    "maxCycleLossUSDT": 150,
    "disableSymbolAfterCriticalCloseFailure": true
  }
}
```

The risk controller should see all flow legs for the same `symbol + settle_time`.

## P2 Details

### Runtime Trap Wall Verification

If using OB-assisted Trap:

- Verify wall still exists immediately before order placement.
- Cancel or skip if wall disappears.
- Record `wall_verified`, `wall_age_ms`, `wall_distance_pct`.

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
