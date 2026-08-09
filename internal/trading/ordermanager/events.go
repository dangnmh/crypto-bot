package ordermanager

import (
	"fmt"
	"time"

	shared "crypto-bot/internal/domain"
)

// Event Topics for Generic Order Manager Micro-Event Execution Pipeline.
const (
	TopicOrderIntent            = "ordermanager.micro.intent"
	TopicOrderPreFlightDone     = "ordermanager.micro.preflight_done"
	TopicOrderFireWindowReached = "ordermanager.micro.fire_window_reached"
	TopicOrderSubmitted         = "ordermanager.micro.submitted"
	TopicOrderTPSLDispatched    = "ordermanager.micro.tpsl_dispatched"
	TopicOrderTimeoutScheduled  = "ordermanager.micro.timeout_scheduled"
	TopicOrderOutcomeResolved   = "ordermanager.micro.outcome_resolved"
	TopicOrderTimeoutExpired    = "ordermanager.micro.timeout_expired"
	TopicOrderBailoutExecuted   = "ordermanager.micro.bailout_executed"
	TopicOrderCompleted         = "ordermanager.micro.completed"
	TopicOrderTradeRecord       = "ordermanager.trade_record"
)

// OrderEvent interface defines standard getters for execution events.
type OrderEvent interface {
	GetReqID() string
	GetClientOrderID() string
	GetSymbol() string
	GetExchange() string
	GetMarketType() MarketType
	GetStrategyType() StrategyType
	GetPreTopic() string
	GetNextTopic() string
	GetTimestamp() time.Time
	GetTopic() string
	ShouldNotify() bool
	GetNotifyMessage() string
	DeduplicateKey() string
}

// MarketType identifies whether the target market is Spot or Future/Perpetual.
type MarketType string

const (
	MarketTypeSpot   MarketType = "SPOT"
	MarketTypeFuture MarketType = "FUTURE"
)

func (m MarketType) String() string {
	if m == "" {
		return string(MarketTypeFuture)
	}
	return string(m)
}

// StrategyType identifies the trading strategy originating the execution.
type StrategyType string

const (
	StrategyFundingReversion StrategyType = "FUNDING_REVERSION"
	StrategyFundingArbitrage StrategyType = "FUNDING_ARBITRAGE"
	StrategyPennyJumper      StrategyType = "PENNY_JUMPER"
	StrategyGrid             StrategyType = "GRID"
	StrategyUnknown          StrategyType = "UNKNOWN"
)

// OrderType represents the exchange order type.
type OrderType string

const (
	OrderTypeIOC      OrderType = "IOC"
	OrderTypePostOnly OrderType = "POST_ONLY"
	OrderTypeLimit    OrderType = "LIMIT"
	OrderTypeMarket   OrderType = "MARKET"
)

// OrderOutcome represents the execution result.
type OrderOutcome string

const (
	OutcomeFilled         OrderOutcome = "filled"
	OutcomePartialFilled  OrderOutcome = "partial_filled"
	OutcomeCanceledNoFill OrderOutcome = "canceled_no_fill"
	OutcomeUnknown        OrderOutcome = "unknown"
)

// BaseExecutionEvent holds common identification and notification control fields for all events.
type BaseExecutionEvent struct {
	ReqID         string       `json:"req_id,omitempty"`
	ClientOrderID string       `json:"client_order_id,omitempty"`
	Symbol        string       `json:"symbol"`
	Exchange      string       `json:"exchange,omitempty"`
	MarketType    MarketType   `json:"market_type,omitempty"`
	StrategyType  StrategyType `json:"strategy_type,omitempty"`
	PreTopic      string       `json:"pre_topic,omitempty"`
	NextTopic     string       `json:"next_topic,omitempty"`
	SendNotify    bool         `json:"send_notify,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
}

func (b BaseExecutionEvent) GetReqID() string         { return b.ReqID }
func (b BaseExecutionEvent) GetClientOrderID() string { return b.ClientOrderID }
func (b BaseExecutionEvent) GetSymbol() string        { return b.Symbol }
func (b BaseExecutionEvent) GetExchange() string      { return b.Exchange }
func (b BaseExecutionEvent) GetMarketType() MarketType {
	if b.MarketType == "" {
		return MarketTypeFuture
	}
	return b.MarketType
}
func (b BaseExecutionEvent) GetStrategyType() StrategyType { return b.StrategyType }
func (b BaseExecutionEvent) GetPreTopic() string           { return b.PreTopic }
func (b BaseExecutionEvent) GetNextTopic() string          { return b.NextTopic }
func (b BaseExecutionEvent) GetTimestamp() time.Time       { return b.Timestamp }
func (b BaseExecutionEvent) GetTopic() string              { return TopicOrderCompleted }
func (b BaseExecutionEvent) ShouldNotify() bool            { return b.SendNotify }

func (b BaseExecutionEvent) GetNotifyMessage() string {
	return fmt.Sprintf("🔔 [%s] Order Event: %s | Symbol: %s | Exchange: %s | Market: %s | ReqID: %s | ClientOID: %s",
		b.StrategyType, b.GetTopic(), b.Symbol, b.Exchange, b.GetMarketType(), b.ReqID, b.ClientOrderID)
}

// DeduplicateKey returns unique key for Watermill EventBus deduplication middleware.
func (b BaseExecutionEvent) DeduplicateKey() string {
	return fmt.Sprintf("%s-%s", b.ReqID, b.NextTopic)
}

// OrderIntentEvent initiates execution workflow.
type OrderIntentEvent struct {
	BaseExecutionEvent
	Side            shared.Side         `json:"side"`
	OrderType       OrderType           `json:"order_type"`
	Price           float64             `json:"price"`
	Volume          float64             `json:"volume"`
	ContractSize    float64             `json:"contract_size"`
	MarginMode      shared.MarginMode   `json:"margin_mode"`
	PositionMode    shared.PositionMode `json:"position_mode"`
	Leverage        int                 `json:"leverage"`
	TakeProfitPrice float64             `json:"take_profit_price,omitempty"`
	StopLossPrice   float64             `json:"stop_loss_price,omitempty"`
	FireTime        time.Time           `json:"fire_time"`
	TimeoutDuration time.Duration       `json:"timeout_duration,omitempty"`
}

func (e OrderIntentEvent) GetTopic() string { return TopicOrderIntent }

// OrderPreFlightCompletedEvent indicates margin mode, position mode, leverage risk limit ready.
type OrderPreFlightCompletedEvent struct {
	OrderIntentEvent
	AdjustedLeverage int       `json:"adjusted_leverage"`
	PreFlightDoneAt  time.Time `json:"pre_flight_done_at"`
}

func (e OrderPreFlightCompletedEvent) GetTopic() string { return TopicOrderPreFlightDone }

// OrderFireWindowReachedEvent indicates precision fire window offset reached.
type OrderFireWindowReachedEvent struct {
	OrderPreFlightCompletedEvent
	FireWindowReachedAt time.Time `json:"fire_window_reached_at"`
}

func (e OrderFireWindowReachedEvent) GetTopic() string { return TopicOrderFireWindowReached }

// OrderSubmittedEvent indicates order dispatched to exchange.
type OrderSubmittedEvent struct {
	BaseExecutionEvent
	Price         float64   `json:"price"`
	Volume        float64   `json:"volume"`
	TPSLSubmitted bool      `json:"tpsl_submitted"`
	SubmittedAt   time.Time `json:"submitted_at"`
}

func (e OrderSubmittedEvent) GetTopic() string { return TopicOrderSubmitted }

// OrderTPSLDispatchedEvent indicates post-fill TP/SL trigger orders placed.
type OrderTPSLDispatchedEvent struct {
	BaseExecutionEvent
	TakeProfitPrice float64   `json:"take_profit_price"`
	StopLossPrice   float64   `json:"stop_loss_price"`
	DispatchedAt    time.Time `json:"dispatched_at"`
}

func (e OrderTPSLDispatchedEvent) GetTopic() string { return TopicOrderTPSLDispatched }

// OrderTimeoutScheduledEvent indicates timeout guard timer started.
type OrderTimeoutScheduledEvent struct {
	BaseExecutionEvent
	Duration    time.Duration `json:"duration"`
	ScheduledAt time.Time     `json:"scheduled_at"`
}

func (e OrderTimeoutScheduledEvent) GetTopic() string { return TopicOrderTimeoutScheduled }

// OrderOutcomeResolvedEvent indicates fill outcome confirmed via WS update + backoff REST poll.
type OrderOutcomeResolvedEvent struct {
	BaseExecutionEvent
	Outcome    OrderOutcome `json:"outcome"`
	FilledVol  float64      `json:"filled_vol"`
	AvgPrice   float64      `json:"avg_price"`
	Reason     string       `json:"reason,omitempty"`
	ResolvedAt time.Time    `json:"resolved_at"`
}

func (e OrderOutcomeResolvedEvent) GetTopic() string { return TopicOrderOutcomeResolved }

// OrderTimeoutExpiredEvent indicates post-settle timeout expired while position is open.
type OrderTimeoutExpiredEvent struct {
	BaseExecutionEvent
	HoldVol   float64   `json:"hold_vol"`
	ExpiredAt time.Time `json:"expired_at"`
}

func (e OrderTimeoutExpiredEvent) GetTopic() string { return TopicOrderTimeoutExpired }

// OrderBailoutExecutedEvent indicates emergency position force-close executed.
type OrderBailoutExecutedEvent struct {
	BaseExecutionEvent
	Side            shared.Side `json:"side"`
	Volume          float64     `json:"volume"`
	ExitPrice       float64     `json:"exit_price"`
	CloseRetryCount int         `json:"close_retry_count,omitempty"`
	Reason          string      `json:"reason"`
	ExecutedAt      time.Time   `json:"executed_at"`
}

func (e OrderBailoutExecutedEvent) GetTopic() string { return TopicOrderBailoutExecuted }

// ShouldNotify returns true for emergency bailouts.
func (e OrderBailoutExecutedEvent) ShouldNotify() bool { return true }

func (e OrderBailoutExecutedEvent) GetNotifyMessage() string {
	return fmt.Sprintf("🚨 [%s] Emergency Bailout Executed | Symbol: %s | Side: %s | Vol: %.4f | Retries: %d | Reason: %s | ReqID: %s",
		e.StrategyType, e.Symbol, e.Side, e.Volume, e.CloseRetryCount, e.Reason, e.ReqID)
}

// OrderCompletedEvent indicates execution workflow terminal state reached.
type OrderCompletedEvent struct {
	BaseExecutionEvent
	Outcome         string    `json:"outcome"`
	EntryPrice      float64   `json:"entry_price"`
	ExitPrice       float64   `json:"exit_price"`
	Volume          float64   `json:"volume"`
	ContractSize    float64   `json:"contract_size,omitempty"`
	GrossProfit     float64   `json:"gross_profit"`
	NetProfit       float64   `json:"net_profit"`
	PnLPct          float64   `json:"pnl_pct"`
	Fee             float64   `json:"fee"`
	FundingFee      float64   `json:"funding_fee,omitempty"`
	CloseRetryCount int       `json:"close_retry_count,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	CompletedAt     time.Time `json:"completed_at"`
}

func (e OrderCompletedEvent) GetTopic() string { return TopicOrderCompleted }

func (e OrderCompletedEvent) ShouldNotify() bool {
	return e.SendNotify || e.Outcome == string(OutcomeFilled) || e.Outcome == string(OutcomePartialFilled)
}

func (e OrderCompletedEvent) GetNotifyMessage() string {
	return fmt.Sprintf("✅ [%s] Order Completed | Symbol: %s | Outcome: %s | NetPnL: %.4f USDT (%.2f%%) | Fee: %.4f | FundingFee: %.4f | ReqID: %s",
		e.StrategyType, e.Symbol, e.Outcome, e.NetProfit, e.PnLPct, e.Fee, e.FundingFee, e.ReqID)
}

// OrderTradeRecordEvent indicates full trade execution & PnL persistence event.
type OrderTradeRecordEvent struct {
	BaseExecutionEvent

	ClientOrderID   string `json:"client_order_id,omitempty"`
	ExchangeOrderID string `json:"exchange_order_id,omitempty"`
	MarketType      string `json:"market_type,omitempty"`
	Side            string `json:"side"`

	// Configuration & Position
	MarginUSDT float64 `json:"margin_usdt,omitempty"`
	Leverage   int     `json:"leverage,omitempty"`

	// Execution & Latency Metrics
	LatencyRTTMs   int64   `json:"latency_rtt_ms,omitempty"`
	ActualSlippage float64 `json:"actual_slippage,omitempty"`

	// Performance & PnL
	OrderType    string  `json:"order_type"`
	EntryPrice   float64 `json:"entry_price"`
	ExitPrice    float64 `json:"exit_price"`
	OrderVol     float64 `json:"order_vol"`
	FilledVol    float64 `json:"filled_vol"`
	ContractSize float64 `json:"contract_size,omitempty"`

	NotionalUSD    float64 `json:"notional_usd"`
	GrossPnL       float64 `json:"gross_pnl"`
	NetPnL         float64 `json:"net_pnl"`
	PnLPct         float64 `json:"pnl_pct"`
	Fee            float64 `json:"fee"`
	FundingFee     float64 `json:"funding_fee,omitempty"`
	HoldDurationMs int64   `json:"hold_duration_ms"`

	// Emergency Risk & Termination Status
	CloseRetryCount     int       `json:"close_retry_count,omitempty"`
	ForceCloseAttempted bool      `json:"force_close_attempted"`
	ForceCloseSucceeded bool      `json:"force_close_succeeded"`
	Outcome             string    `json:"outcome"`
	Status              string    `json:"status"`
	Reason              string    `json:"reason,omitempty"`
	RecordedAt          time.Time `json:"recorded_at"`
}

func (e OrderTradeRecordEvent) GetTopic() string { return TopicOrderTradeRecord }
