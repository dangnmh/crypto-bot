# Penny Jumper Strategy Docs

Tài liệu này là entry point cho business logic và technical contract của Penny Jumper bot. Pattern tổ chức follow `docs/business/funding`: mỗi file cấp một là source of truth cho một mảng rõ ràng, có status, concern, câu hỏi mở và backlog tại đúng nơi mô tả behavior.

## Reading Order

1. [flow.md](flow.md) để nắm lifecycle tổng, event topics và terminal states.
2. [analyze.md](analyze.md) để hiểu thesis chiến thuật, thị trường mục tiêu, edge và risk.
3. [wall_trust_score.md](wall_trust_score.md) để đọc scoring contract cho wall thật/giả.
4. [architecture.md](architecture.md) để đọc runtime architecture, event bus, stores, FSM và safety rules.
5. [implementation_plan.md](implementation_plan.md) để xem scope triển khai, module map và verification plan.

## Document Map

### Runtime Contracts

| File | Vai trò | Status |
|---|---|---|
| [flow.md](flow.md) | Lifecycle chung: pre-filter, depth stream, wall detection, scoring, maker jump, monitoring, exit | Design contract |
| [wall_trust_score.md](wall_trust_score.md) | Thuật toán chấm điểm wall 0-100, factor weights, penalty, heuristic giới hạn bởi MEXC depth stream | Heuristic seed |
| [architecture.md](architecture.md) | Kiến trúc isolated bot, stores, pub/sub topics, FSM, execution/risk layer | Target architecture |

### Strategy And Delivery

| File | Vai trò | Status |
|---|---|---|
| [analyze.md](analyze.md) | Thesis altcoin/low-cap, risk model, sizing, fees, PnL calculator | Strategy analysis |
| [implementation_plan.md](implementation_plan.md) | Module-level delivery plan, dependency choices, tests, manual verification | Planning |

## Strategy Surface

| Flow | Status | Timing | Primary topic | Primary doc |
|---|---|---|---|---|
| Wide-net scan | Design contract | Every ticker job cycle | `penny_jumper.scan.filtered` | [flow.md](flow.md) |
| Wall qualification | Design contract | On depth updates | `penny_jumper.wall.qualified` | [wall_trust_score.md](wall_trust_score.md) |
| Jump workflow | Design contract | Per qualified wall | `penny_jumper.workflow.spawned` | [flow.md](flow.md) |
| Exit workflow | Design contract | After fill until terminal | `penny_jumper.position.closed` | [flow.md](flow.md) |

## Shared Rule

Penny Jumper là bot độc lập:

1. Có `cmd/penny_jumper/main.go` riêng.
2. Có config, WebSocket pool, Local Store, event bus và lifecycle riêng.
3. Không chia sẻ runtime state, orderbook store hoặc WebSocket connection với Funding bot.
4. Các infrastructure package có thể tái sử dụng, nhưng mỗi bot tự khởi tạo instance riêng.

## Unit Convention

| Context | Example | Meaning |
|---|---:|---|
| User-facing config `*Pct` fields | `0.3` | 0.3%, unless field docs explicitly say decimal |
| Internal decimal ratio values | `0.003` | 0.3% |
| Wall distance threshold | `1.0` | 1% from best bid/ask |
| Spread threshold | `0.3` | 0.3% max preferred spread |
| Trust score | `65` | 65/100 score, not percent PnL |

Khi thêm config mới, field user-facing nên ghi rõ unit. Nếu input là percent, convert sang decimal đúng một lần tại boundary config/domain.

## Current Priority

| Priority | Work | Reason |
|---|---|---|
| P1 | Chốt event contract + FSM terminal journal trước khi code trading thật | Bot phản ứng theo event; thiếu terminal state sẽ rất khó debug fill/cancel/bailout |
| P1 | Validate MEXC WebSocket topic/wildcard behavior trước khi rely vào `wall:*` subscription | `cskr/pubsub` wildcard support và exchange stream shape phải khớp runtime thật |
| P2 | Paper-trading recorder cho wall, score, order decision, cancel/bailout latency | Strategy cần dữ liệu thực để tune threshold |

## Documentation Rules

| Rule | Reason |
|---|---|
| Mỗi file có `Status` rõ ràng | Tránh nhầm strategy idea với production behavior |
| Flow doc chỉ chứa lifecycle và terminal contract | Tránh trộn heuristic scoring vào orchestration |
| Scoring logic nằm trong `wall_trust_score.md` | Score là primitive shared bởi detector/workflow/risk |
| Architecture doc giữ module boundaries và safety rules | Code triển khai phải map được sang Clean Architecture |
| Backlog/risk nằm ngay trong doc liên quan | Source of truth không bị tách thành bãi đỗ ý tưởng |

## Source Of Truth

- [flow.md](flow.md) giữ lifecycle tổng, topics, state transition và terminal categories.
- [wall_trust_score.md](wall_trust_score.md) giữ scoring formula, weights, penalty và heuristic limits.
- [architecture.md](architecture.md) giữ runtime boundaries, stores, pub/sub bus và fault tolerance.
- [analyze.md](analyze.md) giữ business thesis, market risk, sizing và PnL assumptions.
- [implementation_plan.md](implementation_plan.md) giữ delivery plan và verification plan.
