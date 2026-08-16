package ordermanager

import (
	"fmt"
	"math"
	"strings"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/formatutil"

	"gorm.io/datatypes"
)

// Event Topics for Generic Order Manager Micro-Event Execution Pipeline.
const (
	TopicOrderIntent                 = "ordermanager.micro.intent"
	TopicOrderPreFlightDone          = "ordermanager.micro.preflight_done"
	TopicOrderFireWindowReached      = "ordermanager.micro.fire_window_reached"
	TopicOrderSubmitted              = "ordermanager.micro.submitted"
	TopicOrderResting                = "ordermanager.micro.resting"
	TopicOrderTPSLDispatched         = "ordermanager.micro.tpsl_dispatched"
	TopicOrderPositionWatchReady     = "ordermanager.micro.position_watch_ready"
	TopicOrderFilled                 = "ordermanager.micro.order_filled"
	TopicOrderPositionClosed         = "ordermanager.micro.position_closed"
	TopicOrderTimeoutScheduled       = "ordermanager.micro.timeout_scheduled"
	TopicOrderTimeoutPositionChecked = "ordermanager.micro.timeout_position_checked"
	TopicOrderOutcomeResolved        = "ordermanager.micro.outcome_resolved"
	TopicOrderTimeoutExpired         = "ordermanager.micro.timeout_expired"
	TopicOrderBailoutExecuted        = "ordermanager.micro.bailout_executed"
	TopicOrderAborted                = "ordermanager.micro.aborted"
	TopicOrderCompleted              = "ordermanager.micro.completed"
	TopicOrderTradeRecord            = "ordermanager.trade_record"
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
	StrategyObfuscator       StrategyType = "OBFUSCATOR"
	StrategyDilution         StrategyType = "DILUTION"
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
	OutcomeCanceled       OrderOutcome = "canceled"
	OutcomeAborted        OrderOutcome = "aborted"
	OutcomeResting        OrderOutcome = "resting"
	OutcomeUnknown        OrderOutcome = "unknown"
)

const (
	StatusAborted   = "aborted"
	StatusCompleted = "completed"
)

// BaseExecutionEvent holds common identification and notification control fields for all events.
type BaseExecutionEvent struct {
	ReqID         string       `json:"req_id,omitempty"`
	RefID         string       `json:"ref_id,omitempty"`
	ClientOrderID string       `json:"client_order_id,omitempty"`
	Symbol        string       `json:"symbol"`
	Exchange      string       `json:"exchange,omitempty"`
	MarketType    MarketType   `json:"market_type,omitempty"`
	StrategyType  StrategyType `json:"strategy_type,omitempty"`
	PreTopic      string       `json:"pre_topic,omitempty"`
	NextTopic     string       `json:"next_topic,omitempty"`
	Timestamp     time.Time    `json:"timestamp"`
}

func (b BaseExecutionEvent) GetReqID() string { return b.ReqID }
func (b BaseExecutionEvent) GetClientOrderID() string {
	return b.ClientOrderID
}
func (b BaseExecutionEvent) GetSymbol() string   { return b.Symbol }
func (b BaseExecutionEvent) GetExchange() string { return b.Exchange }
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
func (b BaseExecutionEvent) ShouldNotify() bool            { return false }

func (b BaseExecutionEvent) GetNotifyMessage() string {
	return ""
}

// DeduplicateKey returns unique key for Watermill EventBus deduplication middleware.
func (b BaseExecutionEvent) DeduplicateKey() string {
	return fmt.Sprintf("%s-%s", b.ReqID, b.NextTopic)
}

// OrderIntentEvent initiates execution workflow.
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

// OrderPositionWatchReadyEvent indicates position stream watcher registered BEFORE order execution.
type OrderPositionWatchReadyEvent struct {
	OrderFireWindowReachedEvent
	Timeout      time.Duration `json:"timeout"`
	WatchReadyAt time.Time     `json:"watch_ready_at"`
}

func (e OrderPositionWatchReadyEvent) GetTopic() string { return TopicOrderPositionWatchReady }

// OrderSubmittedEvent indicates order dispatched to exchange.
type OrderSubmittedEvent struct {
	OrderPositionWatchReadyEvent
	OrderID       string    `json:"order_id,omitempty"`
	Price         float64   `json:"price"`
	Volume        float64   `json:"volume"`
	TPSLSubmitted bool      `json:"tpsl_submitted"`
	SubmittedAt   time.Time `json:"submitted_at"`
}

func (e OrderSubmittedEvent) GetTopic() string   { return TopicOrderSubmitted }
func (e OrderSubmittedEvent) ShouldNotify() bool { return true }

func (e OrderSubmittedEvent) GetNotifyMessage() string {
	stratName := strings.ToUpper(string(e.StrategyType))

	sizeUSD := e.Price * e.Volume
	if e.ContractSize > 0 {
		sizeUSD *= e.ContractSize
	}

	leverage := e.Leverage
	marginUSD := e.MarginUSDT

	fundingRate := e.FundingRate
	vol24h := e.Vol24hUSDT

	orderID := e.OrderID
	frSign := ""
	if fundingRate > 0 {
		frSign = "+"
	}
	frStr := fmt.Sprintf("%s%.1f%%", frSign, fundingRate*100)
	vol24Str := fmt.Sprintf("$%s", strings.ToLower(formatutil.FormatCompactUSD(vol24h)))
	sideStr := formatSideString(e.Side)

	return fmt.Sprintf("🟡 [%s] [%s] [SUBMITTED]\n• Symbol: %s | Side: %s\n• Margin: %s USDT | Leverage: %dx\n• Price: %s | Size: %s USDT\n• FR: %s | Vol24h: %s\n• Order ID: %s\n• Client ID: %s\n• Req ID: %s",
		stratName,
		e.Exchange,
		e.Symbol,
		sideStr,
		formatutil.FormatUSDWithCommas(marginUSD),
		leverage,
		formatutil.FormatPriceWithCommas(e.Price),
		formatutil.FormatUSDWithCommas(sizeUSD),
		frStr,
		vol24Str,
		orderID,
		e.ClientOrderID,
		e.ReqID,
	)
}

// OrderTPSLDispatchedEvent indicates post-fill TP/SL trigger orders placed.
type OrderTPSLDispatchedEvent struct {
	BaseExecutionEvent
	TakeProfitPrice float64   `json:"take_profit_price"`
	StopLossPrice   float64   `json:"stop_loss_price"`
	DispatchedAt    time.Time `json:"dispatched_at"`
}

func (e OrderTPSLDispatchedEvent) GetTopic() string { return TopicOrderTPSLDispatched }

// OrderRestingEvent indicates maker limit/postonly order is resting on the order book.
type OrderRestingEvent struct {
	BaseExecutionEvent
	OrderID   string    `json:"order_id"`
	Price     float64   `json:"price"`
	Volume    float64   `json:"volume"`
	RestingAt time.Time `json:"resting_at"`
}

func (e OrderRestingEvent) GetTopic() string { return TopicOrderResting }

// OrderFilledEvent indicates position fill event received.
type OrderFilledEvent struct {
	BaseExecutionEvent
	Side            shared.Side `json:"side"`
	FillPrice       float64     `json:"fill_price"`
	FillVolContract float64     `json:"fill_vol_contract,omitempty"`
	FillVolCoin     float64     `json:"fill_vol_coin,omitempty"`
	VolumeUSDT      float64     `json:"volume_usdt,omitempty"`
	SlippagePct     float64     `json:"slippage_pct,omitempty"`
	FilledAt        time.Time   `json:"filled_at"`
}

func (e OrderFilledEvent) GetTopic() string { return TopicOrderFilled }

// OrderPositionClosedEvent indicates position close event received with PnL metrics.
type OrderPositionClosedEvent struct {
	BaseExecutionEvent
	EntryPrice       float64 `json:"entry_price"`
	ClosePrice       float64 `json:"close_price"`
	CloseVolContract float64 `json:"close_vol_contract,omitempty"`
	CloseVolCoin     float64 `json:"close_vol_coin,omitempty"`
	Reason           string  `json:"reason"`
	GrossProfit      float64 `json:"gross_profit"`
	NetProfit        float64 `json:"net_profit"`
	PnLPct           float64 `json:"pnl_pct"`
	VolumeUSDT       float64 `json:"volume_usdt,omitempty"`
	Fee              float64 `json:"fee"`
	FundingFee       float64 `json:"funding_fee"`
	HoldDurationMs   int64   `json:"hold_duration_ms"`
	Method           string  `json:"method,omitempty"`
}

func (e OrderPositionClosedEvent) GetTopic() string { return TopicOrderPositionClosed }

// OrderTimeoutScheduledEvent indicates timeout guard timer started.
type OrderTimeoutScheduledEvent struct {
	BaseExecutionEvent
	Duration    time.Duration `json:"duration"`
	ScheduledAt time.Time     `json:"scheduled_at"`
}

func (e OrderTimeoutScheduledEvent) GetTopic() string { return TopicOrderTimeoutScheduled }

// OrderTimeoutPositionCheckedEvent indicates position status verified when timeout expires.
type OrderTimeoutPositionCheckedEvent struct {
	BaseExecutionEvent
	Timeout   time.Duration `json:"timeout"`
	HoldVol   float64       `json:"hold_vol"`
	Error     string        `json:"error,omitempty"`
	CheckedAt time.Time     `json:"checked_at"`
}

func (e OrderTimeoutPositionCheckedEvent) GetTopic() string { return TopicOrderTimeoutPositionChecked }

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

// OrderAbortedEvent indicates execution workflow was aborted due to submit error, order cancellation, or failure.
type OrderAbortedEvent struct {
	BaseExecutionEvent
	OrderID   string    `json:"order_id,omitempty"`
	Reason    string    `json:"reason"`
	Error     string    `json:"error,omitempty"`
	AbortedAt time.Time `json:"aborted_at"`
}

func (e OrderAbortedEvent) GetTopic() string   { return TopicOrderAborted }
func (e OrderAbortedEvent) ShouldNotify() bool { return true }

func (e OrderAbortedEvent) GetNotifyMessage() string {
	stratName := strings.ToUpper(string(e.StrategyType))
	reasonStr := e.Reason
	if reasonStr == "" {
		reasonStr = defaultNotAvailable
	}
	errStr := e.Error
	if errStr == "" {
		errStr = "None"
	}
	orderID := e.OrderID
	if orderID == "" {
		orderID = defaultNotAvailable
	}
	clientID := e.ClientOrderID
	if clientID == "" {
		clientID = defaultNotAvailable
	}
	reqID := e.ReqID
	if reqID == "" {
		reqID = defaultNotAvailable
	}

	return fmt.Sprintf("🔴 [%s] [%s] [ABORTED]\n• Symbol: %s\n• Reason: %s\n• Error: %s\n• Order ID: %s\n• Client ID: %s\n• Req ID: %s",
		stratName,
		e.Exchange,
		e.Symbol,
		reasonStr,
		errStr,
		orderID,
		clientID,
		reqID,
	)
}

// OrderCompletedEvent indicates execution workflow terminal state reached.
type OrderCompletedEvent struct {
	BaseExecutionEvent
	Side             shared.Side    `json:"side,omitempty"`
	OrderID          string         `json:"order_id,omitempty"`
	Outcome          OrderOutcome   `json:"outcome"`
	EntryPrice       float64        `json:"entry_price"`
	ExitPrice        float64        `json:"exit_price"`
	CloseVolContract float64        `json:"close_vol_contract,omitempty"`
	CloseVolCoin     float64        `json:"close_vol_coin,omitempty"`
	VolumeUSDT       float64        `json:"volume_usdt,omitempty"`
	ContractSize     float64        `json:"contract_size,omitempty"`
	GrossProfit      float64        `json:"gross_profit"`
	NetProfit        float64        `json:"net_profit"`
	PnLPct           float64        `json:"pnl_pct"`
	Fee              float64        `json:"fee"`
	FundingFee       float64        `json:"funding_fee,omitempty"`
	FundingRate      float64        `json:"funding_rate,omitempty"`
	Vol24hUSDT       float64        `json:"vol_24h_usdt,omitempty"`
	HoldDurationMs   int64          `json:"hold_duration_ms,omitempty"`
	CloseRetryCount  int            `json:"close_retry_count,omitempty"`
	Reason           string         `json:"reason,omitempty"`
	SettleTime       *time.Time     `json:"settle_time,omitempty"`
	Extra            map[string]any `json:"extra,omitempty"`
	CompletedAt      time.Time      `json:"completed_at"`
}

func (e OrderCompletedEvent) GetTopic() string { return TopicOrderCompleted }

func (e OrderCompletedEvent) ShouldNotify() bool { return true }

const defaultNotAvailable = "N/A"

func formatSideString(side shared.Side) string {
	switch side {
	case shared.SideOpenLong, shared.SideCloseLong:
		return "Long"
	case shared.SideOpenShort, shared.SideCloseShort:
		return "Short"
	default:
		raw := side.String()
		if raw != "" {
			return strings.ToUpper(raw[:1]) + strings.ToLower(raw[1:])
		}
		return "Unknown"
	}
}

func formatCompletedPriceString(entryPrice, exitPrice float64, side shared.Side) string {
	var priceDiffPct float64
	if entryPrice > 0 {
		if side == shared.SideOpenShort || side == shared.SideCloseShort {
			priceDiffPct = ((entryPrice - exitPrice) / entryPrice) * 100.0
		} else {
			priceDiffPct = ((exitPrice - entryPrice) / entryPrice) * 100.0
		}
	}
	sign := ""
	if priceDiffPct > 0 {
		sign = "+"
	}
	return fmt.Sprintf("%s ➔ %s (%s%.2f%%)",
		formatutil.FormatPriceWithCommas(entryPrice),
		formatutil.FormatPriceWithCommas(exitPrice),
		sign,
		priceDiffPct,
	)
}

func formatCompletedSizeUSD(entryPrice, exitPrice, volContract, volCoin, contractSize, volumeUSDT float64) string {
	if volumeUSDT > 0 {
		return fmt.Sprintf("%s USDT", formatutil.FormatUSDWithCommas(volumeUSDT))
	}
	price := exitPrice
	if price == 0 {
		price = entryPrice
	}
	if volCoin > 0 && price > 0 {
		return fmt.Sprintf("%s USDT", formatutil.FormatUSDWithCommas(volCoin*price))
	}
	if volContract > 0 && price > 0 {
		cs := contractSize
		if cs <= 0 {
			cs = 1.0
		}
		return fmt.Sprintf("%s USDT", formatutil.FormatUSDWithCommas(volContract*price*cs))
	}
	return "0 USDT"
}

//nolint:cyclop // Formats notification message depending on completion outcome
func (e OrderCompletedEvent) GetNotifyMessage() string {
	stratName := strings.ToUpper(string(e.StrategyType))

	if e.Outcome == (OutcomeCanceledNoFill) || e.Outcome == (OutcomeAborted) || e.Outcome == (OutcomeCanceled) {
		orderID := e.OrderID
		if orderID == "" {
			orderID = defaultNotAvailable
		}
		clientID := e.ClientOrderID
		if clientID == "" {
			clientID = defaultNotAvailable
		}
		reqID := e.ReqID
		if reqID == "" {
			reqID = defaultNotAvailable
		}
		reasonStr := e.Reason
		if reasonStr == "" {
			reasonStr = defaultNotAvailable
		}
		return fmt.Sprintf("🔵 [%s] [%s] [%s]\n• Symbol: %s\n• Outcome: %s\n• Reason: %s\n• Order ID: %s\n• Client ID: %s\n• Req ID: %s",
			stratName,
			e.Exchange,
			strings.ToUpper(string(e.Outcome)),
			e.Symbol,
			e.Outcome,
			reasonStr,
			orderID,
			clientID,
			reqID,
		)
	}

	emoji := "🟢"
	if e.NetProfit < 0 {
		emoji = "🔴"
	}
	sideStr := formatSideString(e.Side)

	fundingRate := e.FundingRate
	vol24h := e.Vol24hUSDT

	frSign := ""
	if fundingRate > 0 {
		frSign = "+"
	}
	frStr := fmt.Sprintf("%s%.1f%%", frSign, fundingRate*100)
	vol24Str := fmt.Sprintf("$%s", strings.ToLower(formatutil.FormatCompactUSD(vol24h)))

	priceStr := formatCompletedPriceString(e.EntryPrice, e.ExitPrice, e.Side)
	sizeStr := formatCompletedSizeUSD(e.EntryPrice, e.ExitPrice, e.CloseVolContract, e.CloseVolCoin, e.ContractSize, e.VolumeUSDT)

	execFeeStr := fmt.Sprintf("$%s", formatutil.FormatUSDWithCommasAndDecimals(math.Abs(e.Fee), 4))
	fundingFeeStr := formatutil.FormatFundingFee(e.FundingFee)

	netPnLStr := formatutil.FormatNetPnL(e.NetProfit, e.PnLPct)
	durationStr := formatutil.FormatDuration(e.HoldDurationMs)

	orderID := e.OrderID
	if orderID == "" {
		orderID = defaultNotAvailable
	}
	clientID := e.ClientOrderID
	if clientID == "" {
		clientID = defaultNotAvailable
	}

	return fmt.Sprintf("%s [%s] [%s] [COMPLETED]\n• Symbol: %s\n• PnL: %s [%s] | Side: %s\n• FR: %s | Vol24h: %s\n• Price: %s | Size: %s\n• Fees: Exec: %s | Funding: %s\n• Order ID: %s\n• Client ID: %s\n• Req ID: %s",
		emoji,
		stratName,
		e.Exchange,
		e.Symbol,
		netPnLStr,
		durationStr,
		sideStr,
		frStr,
		vol24Str,
		priceStr,
		sizeStr,
		execFeeStr,
		fundingFeeStr,
		orderID,
		clientID,
		e.ReqID,
	)
}

// OrderTradeRecordEvent indicates full trade execution & PnL persistence event.
type OrderTradeRecordEvent struct {
	BaseExecutionEvent

	ClientOrderID    string `json:"client_order_id,omitempty"`
	ExchangeOrderID  string `json:"exchange_order_id,omitempty"`
	NormalizedSymbol string `json:"normalized_symbol,omitempty"`
	MarketType       string `json:"market_type,omitempty"`
	Side             string `json:"side"`

	// Configuration & Position
	MarginUSDT float64 `json:"margin_usdt,omitempty"`
	Leverage   int     `json:"leverage,omitempty"`

	// Execution & Latency Metrics
	LatencyRTTMs   int64   `json:"latency_rtt_ms,omitempty"`
	ActualSlippage float64 `json:"actual_slippage,omitempty"`

	// Performance & PnL
	OrderType        string  `json:"order_type"`
	EntryPrice       float64 `json:"entry_price"`
	ExitPrice        float64 `json:"exit_price"`
	OrderVol         float64 `json:"order_vol"`
	FillVolContract  float64 `json:"fill_vol_contract,omitempty"`
	FillVolCoin      float64 `json:"fill_vol_coin,omitempty"`
	CloseVolContract float64 `json:"close_vol_contract,omitempty"`
	CloseVolCoin     float64 `json:"close_vol_coin,omitempty"`
	ContractSize     float64 `json:"contract_size,omitempty"`

	NotionalUSD    float64 `json:"notional_usd"`
	GrossPnL       float64 `json:"gross_pnl"`
	NetPnL         float64 `json:"net_pnl"`
	PnLPct         float64 `json:"pnl_pct"`
	Fee            float64 `json:"fee"`
	FundingFee     float64 `json:"funding_fee,omitempty"`
	HoldDurationMs int64   `json:"hold_duration_ms"`

	// Emergency Risk & Termination Status
	CloseRetryCount     int               `json:"close_retry_count,omitempty"`
	ForceCloseAttempted bool              `json:"force_close_attempted"`
	ForceCloseSucceeded bool              `json:"force_close_succeeded"`
	Outcome             string            `json:"outcome"`
	Status              string            `json:"status"`
	Reason              string            `json:"reason,omitempty"`
	RecordedAt          time.Time         `json:"recorded_at"`
	FireAt              *time.Time        `json:"fire_at,omitempty"`
	SettleTime          *time.Time        `json:"settle_time,omitempty"`
	Extra               datatypes.JSONMap `json:"extra,omitempty"`
}

func (e OrderTradeRecordEvent) GetTopic() string { return TopicOrderTradeRecord }
