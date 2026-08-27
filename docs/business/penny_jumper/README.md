# Penny Jumper Strategy Documentation

This documentation suite serves as the complete technical and business specification for the **Penny Jumper (High-Frequency Micro-Structure Tick-Jumping Bot)**.

---

## Modular Flow Specifications

The strategy architecture is split into six modular, decoupled flows:

| Document | Description | Key Topics & Contracts |
|---|---|---|
| **[01. Universe Discovery & Depth Flow](01_universe_and_depth_flow.md)** | Top 30 gainers polling, dynamic WebSocket depth streams, `DepthStore` (`go-cache`) | `TopicDepthUpdated`, Toobit REST/WS, MEXC REST/WS |
| **[02. Wall Detection Flow](02_wall_detection_flow.md)** | Real-time orderbook depth level scanning, relative volume ratio ($\ge 20\times$), distance & spread filtering, wall lifecycle | `TopicWallDetected`, `TopicWallChanged`, `TopicWallDisappeared` |
| **[03. Wall Trust Scoring Flow](03_wall_trust_scoring_flow.md)** | 6-factor trust scoring ($0 - 100$), anti-spoof penalties, tick-jumping entry price calculation ($1$ tick ahead) | `TopicWallScored`, `TopicWallQualified` |
| **[04. Risk Management & Position Sizing Flow](04_risk_and_position_sizing_flow.md)** | Trust-weighted position sizing, contract calculation, max concurrent positions, daily circuit breaker, cooldown | Pre-flight risk gates, drawdown protection |
| **[05. Order Lifecycle & OrderManager Flow](05_order_lifecycle_and_ordermanager_flow.md)** | Pure event-sourced pipeline, `OrderManager` delegation, post-only maker entry, resting watchdogs, TP targets, wall defense bailouts, telemetry | `TopicOrderIntent`, `TopicOrderCompleted`, `CandidateStore` |
| **[06. Event Sourcing & All-Case Matrix](06_event_sourcing_all_cases_flow.md)** | Exhaustive case-by-case flow (12 scenarios): happy path, anti-spoof resting cancellation, weakened wall defense, partial fills, timeout bailouts, disconnects, daily circuit breakers, and event replay | Master case matrix, state & event transitions |
| **[07. Multi-Exchange OrderBook Sync & Telemetry](07_multi_exchange_orderbook_sync.md)** | Multi-exchange orderbook engine (Toobit snapshot mode, MEXC incremental delta mode, gap recovery via commits), Phase 1 observer mode, PostgreSQL `walls` schema, and critical EOL alerts | `SyncModeSnapshot`, `SyncModeIncremental`, `LocalOrderBook`, `Synchronizer` |
| **[08. Wall Flapping & Repeated Resize Flow](08_wall_flapping_and_resize_flow.md)** | Microstructure stability: grace-period hysteresis (3s) for flickering walls, repeated resize tracking, absorbed vs pulled volume, spoofing history penalties | `WallStatusUnstable`, `FlappingGracePeriod`, `PullCount1h`, `FlapCount` |
| **[09. Wall Event Sourcing & Storage Flow](09_wall_event_sourcing_and_storage_flow.md)** | Immutable wall event journaling, sequential micro-event stream (`WALL_BORN`, `WALL_ABSORBED`, `WALL_RESIZED`), in-memory ring buffer & PostgreSQL training/inference dataset | `TopicWallEventStream`, `penny_jumper_wall_events` |

---

## Strategy Analysis & Architecture References

- **[Architecture Specification](architecture.md)**: Isolated bot architecture, FX module wiring, event bus pub/sub topology, and data stores.
- **[Strategy Analysis](analyze.md)**: Market micro-structure thesis, maker rebate edge, fee models, and expected value.
- **[Wall Trust Score Reference](wall_trust_score.md)**: Deep dive into mathematical weights and heuristic limits.
- **[Master Flow Reference](flow.md)**: High-level overview of the complete bot lifecycle.

---

## Shared Bot Rules
1. **Isolated Bot**: Penny Jumper runs in its own binary (`cmd/penny_jumper/main.go`) with dedicated configs (`configs/penny_jumper/`), `DepthStore`, `CandidateStore`, and `eventbus.Bus`.
2. **Pure Event Sourcing**: All execution flows communicate asynchronously via Watermill `eventbus.Bus` topics.
3. **Execution Delegation**: All exchange order placements, margin adjustments, leverage switches, resting timeouts, and emergency bailouts are managed by `internal/trading/ordermanager`.
