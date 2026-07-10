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
	ID                       uint      `gorm:"primaryKey;autoIncrement"`
	Timestamp                time.Time `gorm:"column:timestamp;index;not null"`
	Exchange                 string    `gorm:"column:exchange;size:32;uniqueIndex:idx_exch_sym_settle;not null"`
	Symbol                   string    `gorm:"column:symbol;size:32;uniqueIndex:idx_exch_sym_settle;not null"`
	NormalizedSymbol         string    `gorm:"column:normalized_symbol;size:32;index;not null"`
	FundingRate              float64   `gorm:"column:funding_rate;type:numeric(10,6);not null"`
	Volume24h                float64   `gorm:"column:volume_24h;type:numeric(20,4);not null"`
	SettleTime               time.Time `gorm:"column:settle_time;uniqueIndex:idx_exch_sym_settle;not null"`
	PreFundingPriceFetched   bool      `gorm:"column:pre_funding_price_fetched;default:false;index;not null"`
	AfterFundingPriceFetched bool      `gorm:"column:after_funding_price_fetched;default:false;index;not null"`
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
//
//nolint:goconst // Column name literals are reused across schema definitions and shouldn't be package-level constants
func (r *GormSymbolFundingReportRepository) SaveBatch(ctx context.Context, reports []domain.SymbolFundingReport) error {
	if len(reports) == 0 {
		return nil
	}
	dbModels := make([]GormSymbolFundingReport, len(reports))
	for i := range reports {
		rep := &reports[i]
		dbModels[i] = GormSymbolFundingReport{
			Timestamp:                rep.Timestamp,
			Exchange:                 rep.Exchange,
			Symbol:                   rep.Symbol,
			NormalizedSymbol:         rep.NormalizedSymbol,
			FundingRate:              rep.FundingRate,
			Volume24h:                rep.Volume24h,
			SettleTime:               rep.SettleTime,
			PreFundingPriceFetched:   rep.PreFundingPriceFetched,
			AfterFundingPriceFetched: rep.AfterFundingPriceFetched,
		}
	}

	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "exchange"}, {Name: "symbol"}, {Name: "settle_time"}},
		DoUpdates: clause.AssignmentColumns([]string{"timestamp", "normalized_symbol", "funding_rate", "volume_24h"}),
	}).Create(&dbModels).Error
}

//nolint:dupl // Pre and after pending queries have identical mapping logic but filter on different status fields.
func (r *GormSymbolFundingReportRepository) GetPendingPreFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	var dbReports []GormSymbolFundingReport
	err := r.db.WithContext(ctx).
		Where("settle_time <= ? AND pre_funding_price_fetched = ?", settleTimeThreshold, false).
		Find(&dbReports).Error
	if err != nil {
		return nil, err
	}
	res := make([]domain.SymbolFundingReport, len(dbReports))
	for i := range dbReports {
		dr := &dbReports[i]
		res[i] = domain.SymbolFundingReport{
			ID:                       dr.ID,
			Timestamp:                dr.Timestamp,
			Exchange:                 dr.Exchange,
			Symbol:                   dr.Symbol,
			NormalizedSymbol:         dr.NormalizedSymbol,
			FundingRate:              dr.FundingRate,
			Volume24h:                dr.Volume24h,
			SettleTime:               dr.SettleTime,
			PreFundingPriceFetched:   dr.PreFundingPriceFetched,
			AfterFundingPriceFetched: dr.AfterFundingPriceFetched,
		}
	}
	return res, nil
}

//nolint:dupl // Pre and after pending queries have identical mapping logic but filter on different status fields.
func (r *GormSymbolFundingReportRepository) GetPendingAfterFunding(ctx context.Context, settleTimeThreshold time.Time) ([]domain.SymbolFundingReport, error) {
	var dbReports []GormSymbolFundingReport
	err := r.db.WithContext(ctx).
		Where("settle_time <= ? AND after_funding_price_fetched = ?", settleTimeThreshold, false).
		Find(&dbReports).Error
	if err != nil {
		return nil, err
	}
	res := make([]domain.SymbolFundingReport, len(dbReports))
	for i := range dbReports {
		dr := &dbReports[i]
		res[i] = domain.SymbolFundingReport{
			ID:                       dr.ID,
			Timestamp:                dr.Timestamp,
			Exchange:                 dr.Exchange,
			Symbol:                   dr.Symbol,
			NormalizedSymbol:         dr.NormalizedSymbol,
			FundingRate:              dr.FundingRate,
			Volume24h:                dr.Volume24h,
			SettleTime:               dr.SettleTime,
			PreFundingPriceFetched:   dr.PreFundingPriceFetched,
			AfterFundingPriceFetched: dr.AfterFundingPriceFetched,
		}
	}
	return res, nil
}

func (r *GormSymbolFundingReportRepository) UpdatePreFunding(ctx context.Context, id uint, fetched bool) error {
	return r.db.WithContext(ctx).
		Model(&GormSymbolFundingReport{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"pre_funding_price_fetched": fetched,
		}).Error
}

func (r *GormSymbolFundingReportRepository) UpdateAfterFunding(ctx context.Context, id uint, fetched bool) error {
	return r.db.WithContext(ctx).
		Model(&GormSymbolFundingReport{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"after_funding_price_fetched": fetched,
		}).Error
}
