# OrderManager Event Flow Diagram & Guide

This document describes the event-driven micro-pipeline architecture of the **Generic OrderManager** (`internal/trading/ordermanager`).

The `OrderManager` isolates complete order execution lifecycle management from strategy logic. It uses a reactive Watermill micro-event topic pipeline over the internal `*eventbus.Bus`.

---

## 1. Micro-Event Flow Diagram (Mermaid Flowchart)

```mermaid
graph TD
    %% Styling
    classDef intent fill:#e8f5e9,stroke:#2e7d32,stroke-width:2px,color:#1b5e20;
    classDef preflight fill:#e3f2fd,stroke:#1565c0,stroke-width:2px,color:#0d47a1;
    classDef timing fill:#fff3e0,stroke:#ef6c00,stroke-width:2px,color:#e65100;
    classDef execute fill:#fce4ec,stroke:#c2185b,stroke-width:2px,color:#880e4f;
    classDef tpsl fill:#f3e5f5,stroke:#7b1fa2,stroke-width:2px,color:#4a148c;
    classDef watcher fill:#e0f7fa,stroke:#00838f,stroke-width:2px,color:#004d40;
    classDef outcome fill:#fff8e1,stroke:#f57f17,stroke-width:2px,color:#f57f17;
    classDef timeout fill:#fbe9e7,stroke:#d84315,stroke-width:2px,color:#bf360c;
    classDef complete fill:#eceff1,stroke:#37474f,stroke-width:2px,color:#263238;
    classDef db fill:#e8eaf6,stroke:#283593,stroke-width:2px,color:#1a237e;
    classDef abort fill:#ffebee,stroke:#c62828,stroke-width:2px,color:#b71c1c;

    subgraph Step 1: Pre-Flight Setup
        STRATEGY[Strategy Dispatch]:::intent -->|ordermanager.micro.intent| A["[Step 1] HandlePreFlight"]:::preflight
        A -->|1. Switch Margin Mode| A1[Exchange API]:::preflight
        A -->|2. Switch Position Mode| A2[Exchange API]:::preflight
        A -->|3. Configure Leverage| A3[Exchange API / Risk Limits]:::preflight
        A -->|ordermanager.micro.preflight_done| B["[Step 2] HandleFireTiming"]:::timing
    end

    subgraph Step 2: Precision Timing Window
        B -->|Check Target FireTime| B1{FireTime > Now?}:::timing
        B1 -->|Yes| B2[clock.Sleep until FireTime]:::timing
        B1 -->|No / Past| B3[Proceed Immediately]:::timing
        B2 -->|ordermanager.micro.fire_window_reached| W["[Step 3] HandlePositionWatchReady"]:::watcher
        B3 -->|ordermanager.micro.fire_window_reached| W
    end

    subgraph Step 3: Stream Watcher Registration
        W -->|Subscribe WS Position Stream| W1[posWatcher.OnPositionUpdate]:::watcher
        W -->|ordermanager.micro.position_watch_ready| C["[Step 4] HandleExecuteOrder"]:::execute
    end

    subgraph Step 4: Order Submission & Simultaneous Parallel Triggers
        C -->|REST client.CreateOrder| C1[Exchange Matching Engine]:::execute
        
        C -->|ordermanager.micro.submitted| D["[Step 5A] HandleTPSLSubmission"]:::tpsl
        C -->|ordermanager.micro.submitted| F["[Step 5B] HandleOutcomeWatcher"]:::outcome
        C -->|ordermanager.micro.submitted| G["[Step 5C] HandleScheduleTimeout"]:::timeout

        D -->|If TP/SL Configured| D1[client.CreateOrder TP/SL]:::tpsl
        F -->|Poll / Stream Status| F1{Outcome Status?}:::outcome
        G -->|AfterFunc Timer| G1[ordermanager.micro.timeout_scheduled]:::timeout
    end

    subgraph Step 5: Position Lifecycle & Timeout Watchdog
        F1 -->|OutcomeCanceledNoFill| H["[Step 6] HandleEnrichAndComplete"]:::complete
        F1 -->|Filled / Position Open| POS[Real-time WS Position Stream]:::watcher
        
        POS -->|HoldVol > 0| P1[ordermanager.micro.filled]:::watcher
        POS -->|HoldVol == 0| P2[ordermanager.micro.position_closed]:::watcher
        P2 --> H

        G1 -->|WaitTimeoutDeadline| T1[ordermanager.micro.timeout_position_checked]:::timeout
        T1 -->|HoldVol > 0: Emergency Close| T2["[Step 6B] HandleExecuteBailout"]:::timeout
        T2 -->|Bailout Success| T3[ordermanager.micro.bailout_executed]:::timeout
        T2 -->|Bailout Error| ABORT[TopicOrderAborted]:::abort
    end

    subgraph Step 6: Trade Enrichment & Persistence
        H -->|Calculate Closed PnL & Fees| H1[ordermanager.micro.completed]:::complete
        H1 -->|agg.BuildTradeRecord| DB["[Step 7] HandleTradeRecordPersistence"]:::db
        DB -->|ordermanager.trade_record| DB1[(PostgreSQL / SQLite Trades Table)]:::db
    end

    %% Styles
    class STRATEGY intent;
    class A,A1,A2,A3 preflight;
    class B,B1,B2,B3 timing;
    class W,W1,POS,P1,P2 watcher;
    class C,C1 execute;
    class D,D1 tpsl;
    class F,F1 outcome;
    class G,G1,T1,T2,T3 timeout;
    class H,H1 complete;
    class DB,DB1 db;
    class ABORT abort;
```

---

## 2. Watermill Micro-Event Pipeline Topics

The micro-event topics defined in [events.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/events.go) drive the execution pipeline in [execute.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/execute.go):

| Step | Topic Constant (`ordermanager.micro...`) | Input Event Struct | Output Event Struct | Handler Location | Primary Action |
|---|---|---|---|---|---|
| **1** | `micro.intent` | `OrderIntentEvent` | `OrderPreFlightCompletedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L293) | Switches margin & position mode; configures exchange leverage. |
| **2** | `micro.preflight_done` | `OrderPreFlightCompletedEvent` | `OrderFireWindowReachedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L340) | Precision sleeps until `evt.FireTime` target timestamp. |
| **3** | `micro.fire_window_reached` | `OrderFireWindowReachedEvent` | `OrderPositionWatchReadyEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L368) | Wires exchange WS position watcher stream callback BEFORE order submission. |
| **4** | `micro.position_watch_ready` | `OrderPositionWatchReadyEvent` | `OrderSubmittedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L544) | Fires primary order to exchange matching engine via REST with WS listener active. |
| **5A** | `micro.submitted` | `OrderSubmittedEvent` | `OrderTPSLDispatchedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L653) | **Simultaneous Parallel**: Submits contingent TP/SL limit/market orders if configured. |
| **5B** | `micro.submitted` | `OrderSubmittedEvent` | `OrderOutcomeResolvedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L794) | **Simultaneous Parallel**: Monitors order fill status via WS stream & backoff poll. If `canceled_no_fill`, completes cycle. |
| **5C** | `micro.submitted` | `OrderSubmittedEvent` | `OrderTimeoutScheduledEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L658) | **Simultaneous Parallel**: Schedules post-fill hold timeout watchdog timer (`time.AfterFunc`). |
| **6** | `micro.timeout_scheduled` | `OrderTimeoutScheduledEvent` | `OrderTimeoutPositionCheckedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L742) | Checks exchange open positions upon timeout expiration. |
| **7** | `micro.timeout_position_checked` | `OrderTimeoutPositionCheckedEvent` | `OrderBailoutExecutedEvent` | [manager.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/manager.go#L936) | Performs emergency force close position with retries if position is open; triggers `abortOrder` on error. |
| **8** | `micro.position_closed` | `OrderPositionClosedEvent` | `OrderCompletedEvent` | [execute.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/execute.go#L220) | Triggers `HandleEnrichAndComplete` when WS reports position closed; computes entry/exit prices, fees, net PnL. |
| **9** | `micro.completed` | `OrderCompletedEvent` | `OrderTradeRecordEvent` | [execute.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/execute.go#L236) | Aggregates uncommitted events into standardized trade record entity and dispatches to persistence. |
| **10** | `trade_record` | `OrderTradeRecordEvent` | *(database row)* | [execute.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/execute.go#L255) | Persists standardized trade record to SQL database `trades` table via GORM upsert. |

---

## 3. Structure & Payload Design

### 3.1. `OrderIntentEvent` ([events.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/events.go#L145-L178))

```go
type OrderIntentEvent struct {
	// Event Identification
	BaseExecutionEvent

	// Order Parameters
	Side         shared.Side `json:"side"`
	OrderType    OrderType   `json:"order_type"`
	Price        float64     `json:"price"`
	Volume       float64     `json:"volume"`
	ContractSize float64     `json:"contract_size"`

	// Exchange & Risk Configuration
	MarginMode   shared.MarginMode   `json:"margin_mode"`
	PositionMode shared.PositionMode `json:"position_mode"`
	Leverage     int                 `json:"leverage"`
	MarginUSDT   float64             `json:"margin_usdt,omitempty"`
	FundingRate  float64             `json:"funding_rate,omitempty"`
	Vol24hUSDT   float64             `json:"vol_24h_usdt,omitempty"`

	// Contingency & Risk Limits
	TakeProfitPrice float64       `json:"take_profit_price,omitempty"`
	StopLossPrice   float64       `json:"stop_loss_price,omitempty"`
	TimeoutDuration time.Duration `json:"timeout_duration,omitempty"`

	// Execution & Timing Targets
	FireTime   time.Time     `json:"fire_time"`
	MaxLatency time.Duration `json:"max_latency,omitempty"`
	SettleTime *time.Time    `json:"settle_time,omitempty"`

	// Additional Metadata & Strategy Specific Info
	Extra map[string]any `json:"extra,omitempty"`
}
```

### 3.2. `OrderTradeRecordEvent` ([events.go](file:///home/four/projects/crypto-bot/internal/trading/ordermanager/events.go#L605-L630))

```go
type OrderTradeRecordEvent struct {
	BaseExecutionEvent
	Side             shared.Side `json:"side"`
	OrderID          string      `json:"order_id"`
	Outcome          string      `json:"outcome"`
	EntryPrice       float64     `json:"entry_price"`
	ExitPrice        float64     `json:"exit_price"`
	VolumeUSDT       float64     `json:"volume_usdt"`
	ContractSize     float64     `json:"contract_size"`
	CloseVolContract float64     `json:"close_vol_contract"`
	CloseVolCoin     float64     `json:"close_vol_coin"`
	GrossProfit      float64     `json:"gross_profit"`
	NetProfit        float64     `json:"net_profit"`
	PnLPct           float64     `json:"pnl_pct"`
	Fee              float64     `json:"fee"`
	FundingFee       float64     `json:"funding_fee"`
	FundingRate      float64     `json:"funding_rate"`
	Vol24hUSDT       float64     `json:"vol_24h_usdt"`
	HoldDurationMs   int64       `json:"hold_duration_ms"`
	Status           string      `json:"status"`
	Reason           string      `json:"reason,omitempty"`
	RecordedAt       time.Time   `json:"recorded_at"`
	FireAt           *time.Time  `json:"fire_at,omitempty"`
	SettleTime       *time.Time  `json:"settle_time,omitempty"`
}
```

---

## 4. Deduplication & Idempotency

All Watermill micro-events implement `DeduplicateKey()` via `BaseExecutionEvent`:
$$\text{DeduplicateKey} = \text{ReqID} - \text{NextTopic}$$
This prevents duplicate execution of any micro-step if messages are re-delivered by the event bus.
