package persistence

import (
	"context"
	"fmt"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/pkg/formatutil"

	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Trade side enum string constants.
const (
	SideLong       = "LONG"
	SideShort      = "SHORT"
	SideOpenLong   = "OPEN_LONG"
	SideOpenShort  = "OPEN_SHORT"
	SideCloseLong  = "CLOSE_LONG"
	SideCloseShort = "CLOSE_SHORT"
)

// TradeRecord is the GORM database entity representing a trade execution and PnL record.
type TradeRecord struct {
	ReqID            string      `gorm:"column:req_id;primaryKey;size:64"`
	ClientOrderID    string      `gorm:"column:client_order_id;size:64;index"`
	ExchangeOrderID  string      `gorm:"column:exchange_order_id;size:64;index"`
	Symbol           string      `gorm:"column:symbol;size:32;index:idx_sym_ex;not null"`
	NormalizedSymbol string      `gorm:"column:normalized_symbol;size:32;index;not null"`
	Exchange         string      `gorm:"column:exchange;size:32;index:idx_sym_ex;not null"`
	MarketType       string      `gorm:"column:market_type;size:16;index:idx_sym_ex;not null;default:'FUTURE'"`
	StrategyType     string      `gorm:"column:strategy_type;size:32;index:idx_sym_ex;not null"`
	Side             shared.Side `gorm:"column:side;size:16;not null"`
	MarginUSDT       float64     `gorm:"column:margin_usdt;type:numeric(16,4)"`
	Leverage         int         `gorm:"column:leverage"`
	LatencyRTTMs     int64       `gorm:"column:latency_rtt_ms"`
	ActualSlippage   float64     `gorm:"column:actual_slippage;type:numeric(10,6)"`
	OrderType        string      `gorm:"column:order_type;size:16;not null"`
	EntryPrice       float64     `gorm:"column:entry_price;type:numeric(20,8)"`
	ExitPrice        float64     `gorm:"column:exit_price;type:numeric(20,8)"`
	OrderVol         float64     `gorm:"column:order_vol;type:numeric(20,8)"`
	FillVolContract  float64     `gorm:"column:fill_vol_contract;type:numeric(20,8)"`
	FillVolCoin      float64     `gorm:"column:fill_vol_coin;type:numeric(20,8)"`
	CloseVolContract float64     `gorm:"column:close_vol_contract;type:numeric(20,8)"`
	CloseVolCoin     float64     `gorm:"column:close_vol_coin;type:numeric(20,8)"`
	ContractSize     float64     `gorm:"column:contract_size;type:numeric(20,8)"`

	NotionalUSD         float64           `gorm:"column:notional_usd;type:numeric(20,8)"`
	GrossPnL            float64           `gorm:"column:gross_pnl;type:numeric(20,8)"`
	NetPnL              float64           `gorm:"column:net_pnl;type:numeric(20,8)"`
	PnLPct              float64           `gorm:"column:pnl_pct;type:numeric(10,6)"`
	Fee                 float64           `gorm:"column:fee;type:numeric(20,8)"`
	FundingFee          float64           `gorm:"column:funding_fee;type:numeric(20,8)"`
	HoldDurationMs      int64             `gorm:"column:hold_duration_ms"`
	CloseRetryCount     int               `gorm:"column:close_retry_count"`
	ForceCloseAttempted bool              `gorm:"column:force_close_attempted;not null;default:false"`
	ForceCloseSucceeded bool              `gorm:"column:force_close_succeeded;not null;default:false"`
	Outcome             string            `gorm:"column:outcome;size:32;not null"`
	Status              string            `gorm:"column:status;size:16;not null;default:'completed'"`
	Reason              string            `gorm:"column:reason;size:255"`
	FireAt              *time.Time        `gorm:"column:fire_at;index"`
	SettleTime          *time.Time        `gorm:"column:settle_time;index"`
	ObfuscatedAt        *time.Time        `gorm:"column:obfuscated_at;index"`
	Extra               datatypes.JSONMap `gorm:"column:extra;type:jsonb"`
	CreatedAt           time.Time         `gorm:"column:created_at;autoCreateTime;index"`
}

// TableName overrides GORM default table name to "trades".
func (TradeRecord) TableName() string {
	return "trades"
}

// ProfitableTradeRecord holds selected trade metrics required for order obfuscation logic.
type ProfitableTradeRecord struct {
	ReqID            string      `gorm:"column:req_id"`
	Exchange         string      `gorm:"column:exchange"`
	Symbol           string      `gorm:"column:symbol"`
	NormalizedSymbol string      `gorm:"column:normalized_symbol"`
	Side             shared.Side `gorm:"column:side"`
	NetProfit        float64     `gorm:"column:net_pnl"`
	MarginUSDT       float64     `gorm:"column:margin_usdt"`
	Leverage         int         `gorm:"column:leverage"`
	NotionalUSD      float64     `gorm:"column:notional_usd"`
	CreatedAt        time.Time   `gorm:"column:created_at"`
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

// GetProfitableTradeRecords fetches trade records with net_pnl >= threshold within the since time window that haven't been obfuscated yet.
func (r *GormTradeRepository) GetProfitableTradeRecords(ctx context.Context, exchange string, threshold float64, since time.Time) ([]ProfitableTradeRecord, error) {
	if r.db == nil {
		return nil, nil
	}
	var records []ProfitableTradeRecord
	err := r.db.WithContext(ctx).
		Table("trades").
		Select("req_id, exchange, symbol, normalized_symbol, side, net_pnl, margin_usdt, leverage, notional_usd, created_at").
		Where("exchange = ? AND net_pnl >= ? AND created_at >= ? AND (obfuscated_at IS NULL)", exchange, threshold, since).
		Order("created_at DESC").
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("fetch profitable trade records: %w", err)
	}
	return records, nil
}

// MarkObfuscated marks a trade record as obfuscated by setting its obfuscated_at timestamp.
func (r *GormTradeRepository) MarkObfuscated(ctx context.Context, reqID string, obfuscatedAt time.Time) error {
	if r.db == nil {
		return nil
	}
	err := r.db.WithContext(ctx).
		Table("trades").
		Where("req_id = ?", reqID).
		Update("obfuscated_at", obfuscatedAt).Error
	if err != nil {
		return fmt.Errorf("mark trade %s as obfuscated: %w", reqID, err)
	}
	return nil
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

	parsedSide, _ := shared.ParseSide(evt.Side)

	trade := &TradeRecord{
		ReqID:            evt.ReqID,
		ClientOrderID:    clientOID,
		ExchangeOrderID:  exchangeOID,
		Symbol:           evt.Symbol,
		NormalizedSymbol: normSym,
		Exchange:         evt.Exchange,
		MarketType:       evt.MarketType,
		StrategyType:     string(evt.StrategyType),
		Side:             parsedSide,
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
		Extra:               evt.Extra,
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
