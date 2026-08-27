# 05. Order Lifecycle & OrderManager Flow

## 1. Overview
The **Order Lifecycle & OrderManager Flow** represents the execution engine of the Penny Jumper bot. Built on a **pure Event-Sourced architecture**, Penny Jumper does not manage custom low-level exchange REST state machines. Instead, it delegates all margin switching, leverage configuration, post-only entry placement, resting watchdog timeouts, Take-Profit orders, position hold watchdogs, and emergency market bailouts to `internal/trading/ordermanager`.

---

## 2. End-to-End Event-Sourced Sequence Diagram

```mermaid
sequenceDiagram
    autonumber
    participant D as WallDetector
    participant S as WallScorer and Risk
    participant C as CandidateStore (go-cache)
    participant B as EventBus
    participant R as PennyJumperRunner
    participant OM as OrderManager Engine
    participant EX as Exchange (Toobit)
    participant JR as JournalRecorder

    Note over D,S: 1. Wall Qualification
    D->>B: TopicWallDetected (Wall candidate)
    B->>R: HandleWallDetected
    R->>S: ScoreWall() and CanSpawnWorkflow()
    S-->>R: Qualified (Score >= 65)
    R->>C: SaveCandidate(Status: INTENT)
    R->>B: TopicWallQualified Event

    Note over R,OM: 2. Dispatch OrderIntent to OrderManager
    B->>R: HandleWallQualified
    R->>B: ordermanager.TopicOrderIntent (Post-Only Maker, UnfilledTimeout: 60s, TPTimeout: 120s)
    B->>OM: HandlePreFlight -> HandleExecuteOrder
    OM->>EX: Switch Leverage and Isolated Margin
    OM->>EX: Place Post-Only Maker Order (1 Tick Ahead of Wall)
    OM->>B: TopicOrderSubmitted Event
    OM->>OM: Start UnfilledCancelTimeout Watchdog (60s)

    Note over OM,EX: 3A. Fill and Position Management
    EX-->>OM: WS Order Filled Event
    OM->>B: TopicOrderFilled Event
    OM->>OM: Cancel Unfilled Watchdog, Start PositionCloseTimeout Watchdog (120s)
    OM->>EX: Place Limit Take-Profit Order (+0.6% target)

    alt TP Limit Order Filled
        EX-->>OM: TP Filled
        OM->>B: TopicOrderOutcomeResolved (OutcomeFilled)
    else TP Timeout Expired (120s) or Wall Disappeared
        OM->>EX: Immediate Market Bailout Close
        OM->>B: TopicOrderBailoutExecuted
    end

    Note over OM,JR: 4. Terminal Completion and Telemetry
    OM->>B: ordermanager.TopicOrderCompleted (NetProfit, PnLPct, Duration, Fees)
    B->>R: HandleOrderCompleted
    R->>S: RiskManager.OnWorkflowCompleted(Symbol, NetProfit)
    R->>JR: RecordWorkflow(JSONL Telemetry)
    R->>C: DeleteCandidate(ReqID)
    R->>R: Trigger OnCompletedHooks (Delayed Unsubscribe)
```

---

## 3. Real-Time Defensive Wall Monitoring

One of Penny Jumper's core safety edges is that it **continuously monitors the protective wall while the order is resting or position is open**:

```mermaid
flowchart TD
    A["Depth Stream Update"] --> B{"Wall Disappeared or Weakened > 50%?"}
    B -->|"No"| C["Continue Normal Resting / Position Watch"]
    B -->|"Yes"| D["Publish TopicWallDisappeared / TopicWallChanged"]
    D --> E["PennyJumperRunner.HandleWallDisappeared"]
    E --> F{"Check CandidateStore: Is Trade In-Flight for Symbol?"}
    F -->|"No"| G["No Active Order to Protect"]
    F -->|"Yes"| H{"Is Entry Order Still Resting Unfilled?"}
    H -->|"Yes: Order Resting"| I["Call OrderManager.CancelOrder - Abort Maker Entry Before Wall Vanishes"]
    H -->|"No: Position Already Open"| J["Trigger Emergency Market Bailout on OrderManager - Close Immediately"]
```

### Safety Rules:
1. **Wall Pulled While Resting**: If the big buyer/seller cancels their wall before retail fills your maker jump, Penny Jumper detects `TopicWallDisappeared` and immediately cancels the resting order on `OrderManager`.
2. **Wall Weakened $> 50\%$**: If the wall size shrinks by more than half (indicating spoof pulling or aggressive dumping), the bot flags `WallStatusWeakened` and cancels the resting order.
3. **Resting Timeout ($60\text{s}$)**: If the post-only order is not filled within `PendingOrderTimeout` ($60\text{s}$), `OrderManager`'s timer cancels the order (`OutcomeCanceledNoFill`).
4. **Take-Profit Timeout ($120\text{s}$)**: Once filled, if the market does not reach the $+0.6\%$ Take-Profit price within `TPTimeout` ($120\text{s}$), `OrderManager` executes a market bailout to close the trade and prevent bag-holding.
5. **Automatic Bailout Pivot (Race Condition Defense)**: If `OrderManager.CancelOrder` is dispatched when a wall disappears, but the resting order was filled in the matching engine moments earlier (returning `OrderAlreadyFilled`), `OrderManager` immediately pivots to an emergency **Market Close Bailout** to prevent holding an unprotected position.
6. **Partial TP Fills & Remainder Volume Protection**: If the limit Take-Profit order is partially filled (e.g. 6/10 contracts) before a timeout or wall collapse occurs, the bailout routine cancels the remaining resting TP order and executes a market close strictly for the **net open balance** ($\text{RemainingVolume} = \text{FilledEntryVolume} - \text{FilledTPVolume}$) using `ReduceOnly: true` to prevent position inversion.

---

## 4. State & Event Transition Matrix

| Current State / Trigger | Incoming Event | Transition / Action | Next System State |
|---|---|---|---|
| **`IDLE`** | `TopicWallQualified` | Saves `TradeCandidate{Status: INTENT}`, emits `TopicOrderIntent` | **`INTENT`** |
| **`INTENT`** | `TopicOrderSubmitted` | `OrderManager` places Post-Only order on Toobit book, starts 60s timer | **`RESTING`** |
| **`RESTING`** | `TopicOrderFilled` | Entry filled. Dispatches TP limit order (+0.6%), starts 120s timer | **`IN_POSITION`** |
| **`RESTING`** | `TopicWallDisappeared` | Wall vanished! Calls `OrderManager.CancelOrder(reqID)` | **`DONE (Canceled)`** |
| **`RESTING`** | 60s Unfilled Timeout | `OrderManager` cancels resting maker order | **`DONE (Timeout)`** |
| **`IN_POSITION`** | TP Limit Filled | Take-Profit achieved (+0.6%). Emits `TopicOrderCompleted` | **`DONE (Filled TP)`** |
| **`IN_POSITION`** | 120s Position Timeout | `OrderManager` fires emergency market close bailout | **`DONE (Bailout)`** |
| **`IN_POSITION`** | `TopicWallDisappeared` | Armor wall breached! Immediate market close bailout | **`DONE (Defensive Bailout)`** |
| **`DONE`** | `TopicOrderCompleted` | Updates `RiskManager`, records JSONL telemetry, clears `CandidateStore` | **`IDLE`** |

---

## 5. Telemetry & Journal Record Schema

Every completed trade produces a structured JSONL record in `logs/penny_jumper_journal.jsonl`:

```json
{
  "workflow_id": "18082026114457BTCTOOBITPENNY_JUMPER",
  "symbol": "BTCUSDT",
  "realized_pnl": 5.50,
  "realized_pnl_pct": 0.60,
  "duration_ms": 2000,
  "estimated_fees_usdt": 0.04,
  "completed_at": "2026-08-18T11:44:57.000Z"
}
```
