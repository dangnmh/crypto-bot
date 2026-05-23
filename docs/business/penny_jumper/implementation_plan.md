# Penny Jumper Implementation Plan

> Status: planning. This document maps the Penny Jumper design to code modules and verification work. It should be updated as implementation lands. Current architecture and flow contracts are in [architecture.md](architecture.md) and [flow.md](flow.md).

## Scope

Build a dedicated `penny_jumper` bot for MEXC futures that:

1. Filters symbols by liquidity and contract metadata.
2. Subscribes to L2 depth for the active universe.
3. Detects and scores orderbook walls.
4. Spawns one per-symbol workflow for qualified walls.
5. Places post-only maker entry one tick ahead of the wall.
6. Cancels, exits, or bails out when the wall/position state requires it.
7. Emits journal-visible terminal states for tuning.

## Community Libraries

| Concern | Candidate | Status |
|---|---|---|
| WebSocket | existing project WS wrapper, `github.com/gorilla/websocket`, or `github.com/gobwas/ws` | choose after inspecting current infrastructure |
| JSON parsing | existing parser first; `github.com/buger/jsonparser` if hot path needs it | benchmark-driven |
| Orderbook structure | simple fixed 20-level arrays first; `github.com/tidwall/btree` only if deeper book needed | keep minimal for `depth.step0` |
| FSM | `github.com/looplab/fsm` or small explicit state machine | prefer simplest testable approach |
| Rate limiting | `golang.org/x/time/rate` | later phase |
| Pub/sub | existing project bus pattern or `github.com/cskr/pubsub` | align with repo |

Do not add a library just because it is listed here. Use the repo's existing patterns unless the implementation has a measured need.

## Proposed File Map

### Entrypoint And Config

| Path | Purpose |
|---|---|
| `cmd/penny_jumper/main.go` | bot entrypoint |
| `configs/penny_jumper/system.jsonc` | exchange/engine/runtime settings |
| `configs/penny_jumper/penny_jumper.jsonc` | strategy thresholds, sizing, risk limits |

### Application Layer

| Path | Purpose |
|---|---|
| `internal/bots/penny_jumper/application/penny_jumper.go` | top-level orchestrator |
| `internal/bots/penny_jumper/application/subscribe_manager.go` | dynamic symbol subscribe/unsubscribe |
| `internal/bots/penny_jumper/application/wall_detector.go` | wall detection and wall lifecycle events |
| `internal/bots/penny_jumper/application/wall_scorer.go` | Wall Trust Score calculation |
| `internal/bots/penny_jumper/application/workflow_manager.go` | active workflow registry and spawn rules |
| `internal/bots/penny_jumper/application/fsm.go` | per-symbol workflow state machine |
| `internal/bots/penny_jumper/application/risk_manager.go` | sizing, max positions, daily loss |
| `internal/bots/penny_jumper/application/order_queue.go` | order command priority and execution |
| `internal/bots/penny_jumper/application/wall_history.go` | depth ring buffer and wall event history |

### Domain Layer

| Path | Purpose |
|---|---|
| `internal/bots/penny_jumper/domain/wall.go` | wall entity and score breakdown |
| `internal/bots/penny_jumper/domain/state.go` | FSM states and terminal reasons |
| `internal/bots/penny_jumper/domain/event.go` | domain event payloads |
| `internal/bots/penny_jumper/domain/config.go` | strategy config value objects |
| `internal/bots/penny_jumper/domain/journal.go` | workflow journal record contract |

### Infrastructure Candidates

| Path | Purpose |
|---|---|
| `internal/infrastructure/ws/multiplexer.go` | reusable WS connection pool |
| `internal/infrastructure/orderbook/*` | reusable orderbook primitives if existing code is insufficient |
| `internal/infrastructure/journal/*` | append-only workflow journal if not shared cleanly |

Infrastructure is reusable, but runtime instances remain bot-owned.

## Delivery Phases

| Phase | Scope | Done criteria |
|---|---|---|
| 1 | Domain events/config/state + scorer unit tests | deterministic score examples pass |
| 2 | Depth store + wall detector + history | detector emits detected/changed/disappeared in tests |
| 3 | Workflow Manager + FSM without real exchange orders | terminal states covered by unit tests |
| 4 | Paper-trading order adapter + journal | decisions and terminal outcomes recorded |
| 5 | Live small-size MEXC integration | cancel/bailout latency measured and bounded |
| 6 | Rate limiter, reconciliation and alerting hardening | close/cancel failures journaled and alerted |

## Initial Config Surface

| Field | Unit | Seed |
|---|---|---:|
| `minVolume24hUSDT` | USDT | exchange/liquidity dependent |
| `maxPairsPerWSConn` | count | `30` |
| `wallSizeMultiplier` | ratio | `20` |
| `maxWallDistancePct` | percent | `1.0` |
| `maxSpreadPct` | percent | `0.3` soft, stricter hard cap configurable |
| `trustScoreThreshold` | score | `65` |
| `maxConcurrentPositions` | count | `3-5` |
| `maxPositionPct` | percent of capital | `2` |
| `pendingOrderTimeoutSec` | seconds | `60` |
| `tpTimeoutSec` | seconds | `120` |
| `trailingActivationPct` | percent | `0.3` |
| `minPartialFillRatio` | decimal or percent, document explicitly | `0.30` internal |

## Verification Plan

### Automated Tests

| Test | Assertion |
|---|---|
| Wall scorer true-wall case | score `>= 65` |
| Wall scorer spoof case | score `< 65` |
| Score clamp | final score stays in `[0, 100]` |
| Wall detector lifecycle | emits detected, changed, disappeared deterministically |
| Subscribe manager active removal | does not unsubscribe active workflow symbol |
| FSM wall gone while pending | cancel terminal |
| FSM wall gone after fill | bailout terminal |
| FSM partial fill below threshold | partial-exit terminal |
| Risk manager duplicate symbol | blocks second workflow |
| Journal builder | every terminal state produces required fields |

### Benchmarks

| Benchmark | Target |
|---|---|
| Depth parse/update | microsecond-scale, no avoidable hot allocations |
| Wall scan over 20 levels | sub-millisecond under expected symbol fanout |
| Event dispatch to active FSM | target `< 1ms` internal dispatch |

### Manual Verification

| Check | Acceptance |
|---|---|
| Paper mode on live MEXC data | wall detections and skips look plausible |
| Small-size live run | wall gone to cancel request target `< 50ms` |
| Multiplexer scaling | stable subscriptions above one connection limit |
| Private order stream | fills reach the correct symbol FSM |
| Fault injection | WS disconnect triggers cancel/bailout path |

## Known Concerns

| Concern | Mitigation |
|---|---|
| Strategy can overfit heuristic score | require journal and report before increasing size |
| Exchange behavior may differ from docs | validate WS payload, rate limits and post-only semantics early |
| Wildcard bus examples may not match chosen library | implement explicit topic subscription if needed |
| Close/cancel critical path is high risk | build bounded retry, alerting and journal before production sizing |

## Backlog

| Priority | Item |
|---|---|
| P1 | Create domain score examples and tests before exchange integration |
| P1 | Define paper-trading journal JSONL schema |
| P1 | Confirm current repo infrastructure that can be reused |
| P2 | Add journal analysis/report command for threshold tuning |
| P2 | Add symbol warning/delist blacklist integration |
