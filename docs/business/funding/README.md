# Funding Strategy Docs

Tài liệu này là entry point cho business logic của funding bot. Mỗi flow doc là source of truth cho cả behavior đã chạy, concern, câu hỏi mở, suggestion và backlog của chính flow đó.

## Reading Order

1. [flow.md](flow.md) để nắm lifecycle tổng và ranh giới giữa các flow.
2. [reversion_flow.md](reversion_flow.md), [trap_flow.md](trap_flow.md), [pre_funding_flow.md](pre_funding_flow.md) để đọc contract từng flow.
3. [price_flow.md](price_flow.md) và [depth.md](depth.md) để đọc shared pricing/orderbook primitives.
4. [analyze.md](analyze.md) và [journal_analysis.md](journal_analysis.md) để hiểu journal schema, report và tuning rule.
5. Đọc phần `Open Questions`, `Known Concerns`, `Backlog` trong từng flow/shared doc liên quan.

## Document Map

### Runtime Contracts

| File | Vai trò | Status |
|---|---|---|
| [flow.md](flow.md) | Lifecycle chung: init, scan candidate, fan-out sang từng topic flow | Implemented overview |
| [reversion_flow.md](reversion_flow.md) | Flow Reversion: IOC tại settle, trailing exit | Implemented |
| [trap_flow.md](trap_flow.md) | Flow Straddle Trap: limit sau settle, trailing riêng, branch terminal journal contract | Implemented + terminal hardening |
| [pre_funding_flow.md](pre_funding_flow.md) | Flow Pre-Funding Wave | Design only |
| [price_flow.md](price_flow.md) | Pricing/volume shared primitives | Shared primitive |
| [depth.md](depth.md) | Cách dùng orderbook đã được chấp nhận | Implemented constraints |

### Journal And Calibration

| File | Vai trò | Status |
|---|---|---|
| [analyze.md](analyze.md) | Cycle Recorder JSONL schema v2, current recorder behavior, future SQLite direction | JSONL implemented |
| [journal_analysis.md](journal_analysis.md) | Playbook biến journal thành quyết định config | Operating playbook |

## Strategy Surface

| Flow | Status | Timing | Topic sau init scan | Primary doc |
|---|---|---|---|---|
| Reversion | Implemented | T-5m scan, T±0 IOC | `funding.reversion.candidate` | [reversion_flow.md](reversion_flow.md) |
| Straddle Trap | Implemented | T-5m scan, T+50ms limit | `funding.trap.candidate` | [trap_flow.md](trap_flow.md) |
| Pre-Funding Wave | Design only | T-20m đến T-1m | `funding.prefunding.candidate` | [pre_funding_flow.md](pre_funding_flow.md) |

## Shared Rule

Ba flow chỉ dùng chung giai đoạn **init scan candidate**:

1. Load config, contract metadata, ticker/funding data.
2. Scan symbol đủ điều kiện theo funding rate và symbol config.
3. Build candidate snapshot có `symbol`, `settle_time`, `funding_rate`, market metadata, và flow eligibility.
4. Push candidate vào topic riêng của từng flow đã enabled.

Sau bước này, mỗi flow tự quản lifecycle, risk, order, fill watcher, trailing, timeout và journal fields riêng. Whole-cycle cleanup không được nhầm Trap branch terminal với Reversion terminal; khi Reversion cleanup chạy, nó phải settle Trap còn mở trước khi ghi journal.

## Percent Unit Convention

| Context | Example | Meaning |
|---|---:|---|
| User-facing config `*Pct` fields | `3` | 3%, normalized internally to `0.03` |
| Internal decimal ratio values | `0.03` | 3% |
| Funding-rate thresholds | `0.3` or `0.003` | Both mean 0.3%; internal value is `0.003` |
| `maxPriceDiffPercent` | `0.8` | 0.8%; remains percent because slippage formulas use percent |
| Journal percent columns | `3.0` | 3%, unless the schema says decimal |

Khi thêm config mới, nếu user nhập `3` cho 3%, tên field nên kết thúc bằng `Pct`. Convert sang decimal đúng một lần ở config/domain boundary. Existing loader also preserves decimal ratio inputs (`0.03`) for compatibility, but docs and examples should prefer user-facing percent values.

## Current Priority

| Priority | Work | Reason |
|---|---|---|
| P1 | Critical close/cancel hardening | Reversion/Trap đã có guard, nhưng cần retry/backoff, journal retry count, và symbol disable wiring rõ ràng hơn |

## Documentation Rules

| Rule | Reason |
|---|---|
| Mỗi flow doc chỉ chứa pipeline, risk, câu hỏi và backlog của flow đó | Tránh tài liệu dạng bãi đỗ ý tưởng |
| Mỗi file có `Status` rõ ràng: implemented, design only, shared primitive, heuristic seed | Tránh nhầm thiết kế với production behavior |
| Topic namespace dùng `funding.<flow>.<event>` | Journal/replay map trực tiếp với runtime |
| Percent field phải ghi rõ user-facing hay internal unit | Giảm lỗi `3` vs `0.03` |
| Khi một item đã implement, cập nhật ngay vào flow/shared doc thay vì giữ trong file backlog riêng | Source of truth nằm tại nơi behavior được mô tả |

## Source Of Truth

- Tài liệu flow chỉ mô tả hành vi đã implement hoặc explicitly marked design.
- Việc chưa làm, câu hỏi mở, rủi ro và suggestion nằm ngay trong flow/shared doc liên quan.
- [flow.md](flow.md) giữ lifecycle tổng, topic, shared scan và cycle-level risk.
- [reversion_flow.md](reversion_flow.md), [trap_flow.md](trap_flow.md), [pre_funding_flow.md](pre_funding_flow.md) giữ contract cụ thể từng flow.
- [price_flow.md](price_flow.md), [depth.md](depth.md), [analyze.md](analyze.md), [journal_analysis.md](journal_analysis.md) giữ primitive/shared analysis.
