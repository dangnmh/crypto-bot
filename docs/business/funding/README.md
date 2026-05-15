# Funding Strategy Docs

Tài liệu này là entry point cho business logic của funding bot. Mục tiêu hiện tại là giữ tài liệu tách bạch giữa:

- Logic đã tồn tại trong code.
- Flow chưa implement.
- Backlog cần làm.
- Câu hỏi cần quyết định.
- Suggestion cải thiện.
- Concern/rủi ro của logic hiện tại.

## Document Map

| File | Vai trò |
|---|---|
| [flow.md](flow.md) | Lifecycle chung: init, scan candidate, fan-out sang từng topic flow |
| [reversion_flow.md](reversion_flow.md) | Flow Reversion: IOC tại settle, trailing exit |
| [trap_flow.md](trap_flow.md) | Flow Straddle Trap: limit sau settle, trailing riêng |
| [pre_funding_flow.md](pre_funding_flow.md) | Flow Pre-Funding Wave: thiết kế riêng, chưa implement |
| [price_flow.md](price_flow.md) | Pricing/volume shared primitives |
| [depth.md](depth.md) | Cách dùng orderbook đã được chấp nhận |
| [analyze.md](analyze.md) | Cycle Recorder design |
| [journal_analysis.md](journal_analysis.md) | Playbook biến journal thành quyết định config |
| [backlog.md](backlog.md) | Work chưa làm hoặc chưa đủ dữ liệu để làm |
| [question.md](question.md) | Câu hỏi mở cần trả lời trước khi design/code |
| [suggest.md](suggest.md) | Suggestion để chuẩn hóa và cải thiện hệ thống |
| [concern.md](concern.md) | Logic hiện tại còn yếu, rủi ro, hoặc dễ vô ích |
| [trap_strategy_guide.md](trap_strategy_guide.md) | Bảng tham khảo Trap depth, chỉ dùng như calibration seed |

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

Sau bước này, mỗi flow tự quản lifecycle, risk, order, fill watcher, trailing, timeout, journal fields và cleanup riêng. Không dùng một event chain duy nhất cho cả Reversion và Trap.

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
| P0 | Chuẩn hóa event topics theo 3 flow | Giảm coupling giữa Reversion, Trap, Pre-Funding |
| P1 | Trap sizing và cycle exposure cap | Trap không nên âm thầm dùng cùng notional với Reversion |
| P1 | Journal report/query | Biến dữ liệu thành config decision |

## Source Of Truth

- Tài liệu flow chỉ mô tả hành vi đã implement hoặc explicitly marked design.
- Ý tưởng chưa làm nằm trong [backlog.md](backlog.md).
- Câu hỏi chưa chốt nằm trong [question.md](question.md).
- Rủi ro của logic đang tồn tại nằm trong [concern.md](concern.md).
- Suggestion cải thiện nằm trong [suggest.md](suggest.md).
