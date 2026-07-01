package persistence

import (
	"context"
	"time"

	"crypto-bot/internal/bots/funding/domain"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GormSymbolFundingReport is the GORM database model for storing funding stats of a symbol.
type GormSymbolFundingReport struct {
	ID               uint      `gorm:"primaryKey;autoIncrement"`
	Timestamp        time.Time `gorm:"column:timestamp;index;not null"`
	Exchange         string    `gorm:"column:exchange;size:32;uniqueIndex:idx_exch_sym_settle;not null"`
	Symbol           string    `gorm:"column:symbol;size:32;uniqueIndex:idx_exch_sym_settle;not null"`
	NormalizedSymbol string    `gorm:"column:normalized_symbol;size:32;index;not null"`
	FundingRate      float64   `gorm:"column:funding_rate;type:numeric(10,6);not null"`
	Volume24h        float64   `gorm:"column:volume_24h;type:numeric(20,4);not null"`
	SettleTime       time.Time `gorm:"column:settle_time;uniqueIndex:idx_exch_sym_settle;not null"`
}

// TableName overrides the default table name for GormSymbolFundingReport.
func (GormSymbolFundingReport) TableName() string {
	return "symbol_funding_reports"
}

// GormSymbolFundingReportRepository implements domain.SymbolFundingReportRepository using GORM.
type GormSymbolFundingReportRepository struct {
	db *gorm.DB
}

// NewGormSymbolFundingReportRepository creates a new instance of GormSymbolFundingReportRepository.
func NewGormSymbolFundingReportRepository(db *gorm.DB) domain.SymbolFundingReportRepository {
	return &GormSymbolFundingReportRepository{db: db}
}

// SaveBatch persists a batch of symbol funding reports with upsert logic on conflict.
func (r *GormSymbolFundingReportRepository) SaveBatch(ctx context.Context, reports []domain.SymbolFundingReport) error {
	if len(reports) == 0 {
		return nil
	}
	dbModels := make([]GormSymbolFundingReport, len(reports))
	for i, rep := range reports {
		dbModels[i] = GormSymbolFundingReport{
			Timestamp:        rep.Timestamp,
			Exchange:         rep.Exchange,
			Symbol:           rep.Symbol,
			NormalizedSymbol: rep.NormalizedSymbol,
			FundingRate:      rep.FundingRate,
			Volume24h:        rep.Volume24h,
			SettleTime:       rep.SettleTime,
		}
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "exchange"}, {Name: "symbol"}, {Name: "settle_time"}},
		DoUpdates: clause.AssignmentColumns([]string{"timestamp", "normalized_symbol", "funding_rate", "volume_24h"}),
	}).Create(&dbModels).Error
}
