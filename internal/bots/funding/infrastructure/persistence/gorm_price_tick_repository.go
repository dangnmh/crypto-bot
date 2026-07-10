package persistence

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormFundingPriceTick is the GORM database model for storing minute-by-minute price ticks.
type GormFundingPriceTick struct {
	ID           uint      `gorm:"primaryKey;autoIncrement"`
	Exchange     string    `gorm:"column:exchange;size:32;uniqueIndex:idx_tick_exch_sym_settle_time;not null"`
	Symbol       string    `gorm:"column:symbol;size:32;uniqueIndex:idx_tick_exch_sym_settle_time;not null"`
	SettleTime   time.Time `gorm:"column:settle_time;uniqueIndex:idx_tick_exch_sym_settle_time;not null"`
	Timestamp    time.Time `gorm:"column:timestamp;uniqueIndex:idx_tick_exch_sym_settle_time;not null"`
	IntervalType string    `gorm:"column:interval_type;size:16;uniqueIndex:idx_tick_exch_sym_settle_time;not null;default:'1m'"`
	Price        float64   `gorm:"column:price;type:numeric(20,8);not null"`
	BidPrice     float64   `gorm:"column:bid_price;type:numeric(20,8);not null"`
	AskPrice     float64   `gorm:"column:ask_price;type:numeric(20,8);not null"`
}

// TableName overrides the default table name for GormFundingPriceTick.
func (GormFundingPriceTick) TableName() string {
	return "funding_price_ticks"
}

// GormFundingPriceTickRepository implements domain.FundingPriceTickRepository using GORM.
type GormFundingPriceTickRepository struct {
	db *gorm.DB
}

// NewGormFundingPriceTickRepository creates a new instance of GormFundingPriceTickRepository.
func NewGormFundingPriceTickRepository(db *gorm.DB) domain.FundingPriceTickRepository {
	return &GormFundingPriceTickRepository{db: db}
}

// SaveBatch persists a batch of price ticks with upsert logic on conflict.
//
//nolint:goconst // Column name literals are reused across schema definitions and shouldn't be package-level constants
func (r *GormFundingPriceTickRepository) SaveBatch(ctx context.Context, ticks []domain.FundingPriceTick) error {
	if len(ticks) == 0 {
		return nil
	}
	dbModels := make([]GormFundingPriceTick, len(ticks))
	for i := range ticks {
		t := &ticks[i]
		dbModels[i] = GormFundingPriceTick{
			Exchange:     t.Exchange,
			Symbol:       t.Symbol,
			SettleTime:   t.SettleTime,
			Timestamp:    t.Timestamp,
			IntervalType: t.IntervalType,
			Price:        t.Price,
			BidPrice:     t.BidPrice,
			AskPrice:     t.AskPrice,
		}
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "exchange"}, {Name: "symbol"}, {Name: "settle_time"}, {Name: "timestamp"}, {Name: "interval_type"}},
		DoUpdates: clause.AssignmentColumns([]string{"price", "bid_price", "ask_price"}),
	}).Create(&dbModels).Error
}

// GetTicksForSettle retrieves all price ticks for a specific symbol settlement window.
func (r *GormFundingPriceTickRepository) GetTicksForSettle(ctx context.Context, exchange, symbol string, settleTime time.Time) ([]domain.FundingPriceTick, error) {
	var dbTicks []GormFundingPriceTick
	err := r.db.WithContext(ctx).
		Where("exchange = ? AND symbol = ? AND settle_time = ?", exchange, symbol, settleTime).
		Order("timestamp ASC").
		Find(&dbTicks).Error
	if err != nil {
		return nil, err
	}

	res := make([]domain.FundingPriceTick, len(dbTicks))
	for i := range dbTicks {
		t := &dbTicks[i]
		res[i] = domain.FundingPriceTick{
			ID:           t.ID,
			Exchange:     t.Exchange,
			Symbol:       t.Symbol,
			SettleTime:   t.SettleTime,
			Timestamp:    t.Timestamp,
			IntervalType: t.IntervalType,
			Price:        t.Price,
			BidPrice:     t.BidPrice,
			AskPrice:     t.AskPrice,
		}
	}
	return res, nil
}
