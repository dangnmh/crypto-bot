package persistence

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/formatutil"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TradeRecord is the GORM database entity representing a trade execution and PnL record.
type TradeRecord struct {
	ReqID            string  `gorm:"column:req_id;primaryKey;size:64"`
	ClientOrderID    string  `gorm:"column:client_order_id;size:64;index"`
	ExchangeOrderID  string  `gorm:"column:exchange_order_id;size:64;index"`
	Symbol           string  `gorm:"column:symbol;size:32;index:idx_sym_ex;not null"`
	NormalizedSymbol string  `gorm:"column:normalized_symbol;size:32;index;not null"`
	Exchange         string  `gorm:"column:exchange;size:32;index:idx_sym_ex;not null"`
	MarketType       string  `gorm:"column:market_type;size:16;index:idx_sym_ex;not null;default:'FUTURE'"`
	StrategyType     string  `gorm:"column:strategy_type;size:32;index:idx_sym_ex;not null"`
	Side             string  `gorm:"column:side;size:16;not null"`
	MarginUSDT       float64 `gorm:"column:margin_usdt;type:numeric(16,4)"`
	Leverage         int     `gorm:"column:leverage"`
	LatencyRTTMs     int64   `gorm:"column:latency_rtt_ms"`
	ActualSlippage   float64 `gorm:"column:actual_slippage;type:numeric(10,6)"`
	OrderType        string  `gorm:"column:order_type;size:16;not null"`
	EntryPrice       float64 `gorm:"column:entry_price;type:numeric(20,8)"`
	ExitPrice        float64 `gorm:"column:exit_price;type:numeric(20,8)"`
	OrderVol         float64 `gorm:"column:order_vol;type:numeric(20,8)"`
	FillVolContract  float64 `gorm:"column:fill_vol_contract;type:numeric(20,8)"`
	FillVolCoin      float64 `gorm:"column:fill_vol_coin;type:numeric(20,8)"`
	CloseVolContract float64 `gorm:"column:close_vol_contract;type:numeric(20,8)"`
	CloseVolCoin     float64 `gorm:"column:close_vol_coin;type:numeric(20,8)"`
	ContractSize     float64 `gorm:"column:contract_size;type:numeric(20,8)"`

	NotionalUSD         float64    `gorm:"column:notional_usd;type:numeric(20,8)"`
	GrossPnL            float64    `gorm:"column:gross_pnl;type:numeric(20,8)"`
	NetPnL              float64    `gorm:"column:net_pnl;type:numeric(20,8)"`
	PnLPct              float64    `gorm:"column:pnl_pct;type:numeric(10,6)"`
	Fee                 float64    `gorm:"column:fee;type:numeric(20,8)"`
	FundingFee          float64    `gorm:"column:funding_fee;type:numeric(20,8)"`
	HoldDurationMs      int64      `gorm:"column:hold_duration_ms"`
	CloseRetryCount     int        `gorm:"column:close_retry_count"`
	ForceCloseAttempted bool       `gorm:"column:force_close_attempted;not null;default:false"`
	ForceCloseSucceeded bool       `gorm:"column:force_close_succeeded;not null;default:false"`
	Outcome             string     `gorm:"column:outcome;size:32;not null"`
	Status              string     `gorm:"column:status;size:16;not null;default:'completed'"`
	Reason              string     `gorm:"column:reason;size:255"`
	FireAt              *time.Time `gorm:"column:fire_at;index"`
	SettleTime          *time.Time `gorm:"column:settle_time;index"`
	CreatedAt           time.Time  `gorm:"column:created_at;autoCreateTime;index"`
}

// TableName overrides GORM default table name to "trades".
func (TradeRecord) TableName() string {
	return "trades"
}

// TradeRepository interface for trade persistence.
type TradeRepository interface {
	Save(ctx context.Context, evt ordermanager.OrderTradeRecordEvent) error
}

// GormTradeRepository implements TradeRepository using GORM.
type GormTradeRepository struct {
	db *gorm.DB
}

// NewGormTradeRepository initializes GormTradeRepository.
func NewGormTradeRepository(db *gorm.DB) *GormTradeRepository {
	return &GormTradeRepository{db: db}
}

// Save executes an idempotent upsert on the trades table.
func (r *GormTradeRepository) Save(ctx context.Context, evt ordermanager.OrderTradeRecordEvent) error {
	if r.db == nil {
		return nil
	}

	clientOID := evt.ClientOrderID
	exchangeOID := evt.ExchangeOrderID
	normSym := evt.NormalizedSymbol
	if normSym == "" && evt.Symbol != "" {
		normSym = formatutil.GetNormalizedSymbol(evt.Symbol)
	}

	trade := &TradeRecord{
		ReqID:            evt.ReqID,
		ClientOrderID:    clientOID,
		ExchangeOrderID:  exchangeOID,
		Symbol:           evt.Symbol,
		NormalizedSymbol: normSym,
		Exchange:         evt.Exchange,
		MarketType:       evt.MarketType,
		StrategyType:     string(evt.StrategyType),
		Side:             evt.Side,
		MarginUSDT:       evt.MarginUSDT,
		Leverage:         evt.Leverage,
		LatencyRTTMs:     evt.LatencyRTTMs,
		ActualSlippage:   evt.ActualSlippage,
		OrderType:        evt.OrderType,
		EntryPrice:       evt.EntryPrice,
		ExitPrice:        evt.ExitPrice,
		OrderVol:         evt.OrderVol,
		FillVolContract:  evt.FillVolContract,
		FillVolCoin:      evt.FillVolCoin,
		CloseVolContract: evt.CloseVolContract,
		CloseVolCoin:     evt.CloseVolCoin,
		ContractSize:     evt.ContractSize,

		NotionalUSD:         evt.NotionalUSD,
		GrossPnL:            evt.GrossPnL,
		NetPnL:              evt.NetPnL,
		PnLPct:              evt.PnLPct,
		Fee:                 evt.Fee,
		FundingFee:          evt.FundingFee,
		HoldDurationMs:      evt.HoldDurationMs,
		CloseRetryCount:     evt.CloseRetryCount,
		ForceCloseAttempted: evt.ForceCloseAttempted,
		ForceCloseSucceeded: evt.ForceCloseSucceeded,
		Outcome:             evt.Outcome,
		Status:              evt.Status,
		Reason:              evt.Reason,
		FireAt:              evt.FireAt,
		SettleTime:          evt.SettleTime,
		CreatedAt:           evt.RecordedAt,
	}

	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(trade).Error

	if err != nil {
		return fmt.Errorf("failed to save trade record: %w", err)
	}
	return nil
}
