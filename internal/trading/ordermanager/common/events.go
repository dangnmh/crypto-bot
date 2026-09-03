package common

import (
	"time"

	"gorm.io/datatypes"
)

// TopicOrderTradeRecord is the topic name for persisted trade records.
const TopicOrderTradeRecord = "ordermanager.trade_record"

// OrderTradeRecordEvent indicates full trade execution & PnL persistence event.
type OrderTradeRecordEvent struct {
	BaseOrderEvent

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
