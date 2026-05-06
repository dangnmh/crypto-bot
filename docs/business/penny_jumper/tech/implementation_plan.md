# Penny Jumper (Front Running) Implementation Plan

Based on the analysis of the business logic and technical architecture, we will implement the "Penny Jump" bot. The bot focuses on low-latency, high-frequency execution to front-run large orders (walls) on Altcoins/Shitcoins. 

As requested, we will rename the bot from `front_running` to **`penny_jumper`** to better reflect its exact technical behavior and to avoid the broad/ambiguous "front running" terminology.

## Community Library Integrations

To accelerate development and ensure high performance, we will integrate battle-tested Go community libraries for the core infrastructure components rather than building from scratch.

- **JSON Parsing:** `github.com/buger/jsonparser` (Zero-allocation, extremely fast, perfect for high-frequency WebSocket updates).
- **WebSockets:** `github.com/gorilla/websocket` (Industry standard, highly reliable) or `github.com/gobwas/ws` (For extreme low-latency zero-allocation needs).
- **Data Structures (Order Book):** `github.com/tidwall/btree` (High-performance B-Tree, ideal for maintaining sorted Order Book levels).
- **State Machine (FSM):** `github.com/looplab/fsm` (Robust, event-driven state machine library).
- **Rate Limiting (Future Phase):** `golang.org/x/time/rate` (Standard library Token Bucket will be integrated later).
- **Pub/Sub (Event Bus):** `github.com/cskr/pubsub` (Lightweight event bus for the Order Watcher).

## System Requirements & Scope

- **Exchange Scope:** The bot will primarily target **MEXC** initially.
- **WebSocket Limits:** The maximum number of pairs per WS connection will be configurable (`max_pairs_per_ws_conn`), allowing the multiplexer to dynamically scale.
- **Rate Limiting:** Token Bucket rate limiting will be **postponed (applied later)**. For the initial version, the bot will run without strict API-level rate limiting, relying on trust-score filtering to naturally reduce API calls.

## Proposed Changes

We will introduce the `penny_jumper` bot following the existing Clean Architecture/DDD structure.

---

### cmd/penny_jumper

The application entrypoint for the new bot.

#### [NEW] [main.go](file:///e:/projects/crypto-bot/cmd/penny_jumper/main.go)
- Initialize the application engine.
- Wire up the new `penny_jumper` specific services (Wall Detector, Scorer, FSM).

---

### configs/penny_jumper

Configuration templates for the bot.

#### [NEW] [system.jsonc](file:///e:/projects/crypto-bot/configs/penny_jumper/system.jsonc)
- Engine configuration (WS endpoints for MEXC, API keys).
- **Configurable limits:** `max_pairs_per_ws_conn` (e.g., set to 30 for MEXC). Rate limit configurations will be added in a later phase.
#### [NEW] [penny_jumper.jsonc](file:///e:/projects/crypto-bot/configs/penny_jumper/penny_jumper.jsonc)
- Strategy specific params (TrustScore thresholds, Capital allocation %, max concurrent positions, minimum 24h volume for pre-filtering).

---

### internal/infrastructure

Upgrades to infrastructure to support HFT requirements.

#### [NEW] [multiplexer.go](file:///e:/projects/crypto-bot/internal/infrastructure/ws/multiplexer.go)
- **WebSocket Connection Pool:** Automatically spawn new WS connections when the number of subscribed pairs exceeds the exchange's limit (e.g., 30 pairs/conn).
- **Library:** Uses `github.com/gorilla/websocket`.

#### [NEW] [orderbook_manager.go](file:///e:/projects/crypto-bot/internal/infrastructure/ws/orderbook_manager.go)
- **L2 OrderBook Manager:** Maintain 20 levels of Order Book internally.
- **Library (Data Structure):** Uses `github.com/tidwall/btree` to keep Bids and Asks sorted for ultra-fast inserts and lookups.
- **Library (JSON):** Uses `github.com/buger/jsonparser` to extract price and volume directly from the WS byte stream without allocating struct overhead.

#### [NEW] [order_watcher.go](file:///e:/projects/crypto-bot/internal/infrastructure/ws/order_watcher.go)
- **User Data Stream Watcher:** Subscribe to personal trade executions. Uses Observer/Pub-Sub pattern to notify the bot's FSM via channels instantly without REST polling.
- **Library:** Uses `github.com/cskr/pubsub` to broadcast trade fills to all active FSMs.

> **⚠️ Isolated Architecture Note:** Các file trong `internal/infrastructure/` là **reusable modules**. Mỗi bot (Penny Jumper, Funding Reversion) tự khởi tạo instance riêng tại runtime với dedicated WS connections và Local Store riêng biệt. Không có bot nào chia sẻ connection hay state với bot khác.

---

### internal/bots/penny_jumper/application

The core application layer. Contains the orchestrator, background jobs, scanner, and per-symbol workflow FSM.

#### [NEW] [penny_jumper.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/penny_jumper.go)
- Top-level orchestrator (equivalent to `sniper.go` in funding_reversion).
- `RunAsBackground()`: Starts Ticker24h Job, Contract Job, WS connections, Subscribe Manager, and **Event Pipeline** (OB Builder, Detector, Scorer, Workflow Manager).
- `Run()`: Blocks until shutdown signal.
- `Stop()`: Graceful shutdown — cancels all active workflows, unsubscribes bus, closes WS connections.

#### [NEW] [subscribe_manager.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/subscribe_manager.go)
- **Dynamic Subscribe/Unsubscribe Manager.** Triggered after each Ticker24h Job cycle.
- Computes `newPairs` and `removedPairs` by diffing current vs previous filtered pairs.
- Unsubscribes `removedPairs` from WS Multiplexer **ONLY IF** there is no active workflow. If active, marks as `pendingRemoval` to avoid killing data while FSM is running.
- Subscribes `newPairs` to `sub.depth.step0` via WS Multiplexer + initializes DepthStore entries.
- Maintains `previousPairs` set for next diff.

#### [NEW] [workflow_manager.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/workflow_manager.go)
- **Manages active per-symbol workflows.** Tracks which pairs currently have a running FSM goroutine.
- Subscribes to `wall:qualified:*` topic from `engine.Bus`.
- `HasActive(pair) bool` — checked before spawning to prevent duplicate workflows.
- `Spawn(pair, wall, score)` — creates a new goroutine running the FSM for that pair.
- Enforces `maxConcurrentPositions` limit from Risk Manager.

#### [NEW] [fsm.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/fsm.go)
- Implements the **non-looping** per-symbol workflow FSM.
- Transitions: `JUMP_PLACED` → `MONITORING` → `FILLED` → `EXIT_STRATEGY` → `DONE`.
- **Spawned by Workflow Manager**, runs 1 cycle, then goroutine exits. No infinite polling loop.
- Subscribes to `wall:changed`, `wall:disappeared`, and `order:filled` events via `engine.Bus` to trigger state transitions instantly.
- **Library:** Uses `github.com/looplab/fsm`.

#### [NEW] [wall_detector.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/wall_detector.go)
- Subscribes to `depth:*` topic on `engine.Bus`.
- Scans the 20 levels for potential walls (Volume >= 20x average, Distance <= 1%).
- Maintains `activeWalls` internal map to detect changes.
- Emits `wall:detected`, `wall:changed`, or `wall:disappeared` to `engine.Bus` based on state diffs.

#### [NEW] [wall_scorer.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/wall_scorer.go)
- Subscribes to `wall:detected:*` topic on `engine.Bus`.
- Implements the **Wall Trust Score** algorithm. Calculates `AgeScore`, `SizeScore`, `AbsorptionScore`, `StabilityScore`, `ContextScore`, and `HistoricalScore`.
- Emits `wall:qualified` event if TrustScore >= 65.

#### [NEW] [risk_manager.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/risk_manager.go)
- Handles position sizing based on TrustScore.
- Enforces max concurrent positions and daily loss limits.

#### [NEW] [order_queue.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/order_queue.go)
- Priority queue for executing orders. Ensures `CANCEL` (Bailout) commands bypass the queue and execute immediately, while `PLACE` commands wait.
- *Note: Token Bucket rate limiting (`golang.org/x/time/rate`) will be implemented in a later phase.*

#### [NEW] [wall_history.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/application/wall_history.go)
- **Ring buffer** (~60 giây) lưu các OB snapshots cho Absorption và Stability scoring.
- **Rolling WallEvent map:** `map[symbol][priceLevel] → []WallEvent` (rolling window 4 giờ) cho HistoricalScore.
- Tracks: wall appearances, disappearances, absorption volume, resize count.
- Used by: `scorer.go` (HistoricalScore, AbsorptionScore, StabilityScore).

---

### internal/bots/penny_jumper/domain

Bot-specific domain models.

#### [NEW] [state.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/domain/state.go)
- Definitions of the Penny Jump FSM states (`JUMP_PLACED`, `MONITORING`, `FILLED`, `EXIT_STRATEGY`, `BAILOUT`, `TRAILING`, `DONE`).

#### [NEW] [wall.go](file:///e:/projects/crypto-bot/internal/bots/penny_jumper/domain/wall.go)
- Domain entity representing an OrderBook Wall, including its historical absorption data and trust score attributes.

---

## Verification Plan

### Automated Tests
1. **Wall Trust Scorer Unit Tests:** Provide mock OrderBook histories (Spoofing vs. True Wall) and assert that the scorer correctly assigns scores (>= 65 for valid walls, < 65 for spoofing).
2. **OrderBook Manager Benchmark:** Write Go benchmarks (`BenchmarkOrderBookUpdate`) to ensure L2 updates take less than 10 microseconds and use zero heap allocations via `buger/jsonparser` and `tidwall/btree`.
3. **FSM Transitions:** Unit test the `looplab/fsm` implementation to ensure it transitions correctly to `BAILOUT` when a wall disappears.

### Manual Verification
1. **Paper Trading on Testnet/Live (Small Size):** Run the bot with minimum position size ($5) on live MEXC shitcoins.
2. **Latency Monitoring:** Monitor logs to verify that the time from Wall Disappearance -> REST API Cancel Order is < 50ms.
3. **Connection Limits:** Verify the WS multiplexer correctly spans multiple connections when subscribing to > 100 pairs.
