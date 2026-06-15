package persistence

import (
	"context"
	"fmt"
	"os"
	"time"

	"crypto-bot/internal/bots/funding/domain"

	"go.uber.org/fx"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ReversionTradeReport is the GORM database model representing a finalized trade reversion cycle.
type ReversionTradeReport struct {
	ReqID              string    `gorm:"column:req_id;primaryKey;size:64"` // client-side externalOrderId (Cycle ID)
	EventID            string    `gorm:"column:event_id;size:64;not null"`
	Timestamp          time.Time `gorm:"column:timestamp;index;not null"`
	SettleTime         time.Time `gorm:"column:settle_time;not null"`
	Exchange           string    `gorm:"column:exchange;size:32;index:idx_ex_sym;not null"`
	Symbol             string    `gorm:"column:symbol;size:32;not null"`
	NormalizedSymbol   string    `gorm:"column:normalized_symbol;size:32;index:idx_ex_sym;not null"`
	Side               string    `gorm:"column:side;size:16;not null"`
	FundingRate        float64   `gorm:"column:funding_rate;type:numeric(10,6);not null"`
	CandidateFoundTime time.Time `gorm:"column:candidate_found_time;not null"`

	// Config settings
	MarginUSDT   float64 `gorm:"column:margin_usdt;type:numeric(16,4);not null"`
	Leverage     int     `gorm:"column:leverage;not null"`
	BufferTimeMs int64   `gorm:"column:buffer_time_ms;not null"`

	// Execution & Latency Fields
	LatencyRTTMs   int64   `gorm:"column:latency_rtt_ms"`
	ActualSlippage float64 `gorm:"column:actual_slippage;type:numeric(10,6)"`
	FireOffsetMs   int64   `gorm:"column:fire_offset_ms"`

	// Order Tracking Fields
	IOCOrderID string `gorm:"column:ioc_order_id;size:64"`
	IOCOutcome string `gorm:"column:ioc_outcome;size:32"`
	IOCReason  string `gorm:"column:ioc_reason;size:255"`

	// Position & Financial Performance Fields
	OrderFilled    bool    `gorm:"column:order_filled;not null"`
	FillPrice      float64 `gorm:"column:fill_price;type:numeric(20,8)"`
	ClosePrice     float64 `gorm:"column:close_price;type:numeric(20,8)"`
	VolumeUSDT     float64 `gorm:"column:volume_usdt;type:numeric(20,8)"`
	GrossProfit    float64 `gorm:"column:gross_profit;type:numeric(20,8)"`
	NetProfit      float64 `gorm:"column:net_profit;type:numeric(20,8)"`
	PnLPct         float64 `gorm:"column:pnl_pct;type:numeric(10,6)"`
	Fee            float64 `gorm:"column:fee;type:numeric(20,8)"`
	HoldFee        float64 `gorm:"column:hold_fee;type:numeric(20,8)"`
	HoldDurationMs int64   `gorm:"column:hold_duration_ms"`
	ExitReason     string  `gorm:"column:exit_reason;size:64"`

	// Risk & Termination Status Fields
	CloseRetryCount     int       `gorm:"column:close_retry_count"`
	ForceCloseAttempted bool      `gorm:"column:force_close_attempted;not null"`
	ForceCloseSucceeded bool      `gorm:"column:force_close_succeeded;not null"`
	Status              string    `gorm:"column:status;size:16;not null"` // "completed", "aborted", "error"
	ErrorMsg            string    `gorm:"column:error_msg;type:text"`
	CreatedAt           time.Time `gorm:"column:created_at;autoCreateTime"`
}

// TableName overrides the table name for GORM.
func (ReversionTradeReport) TableName() string {
	return "reversion_trade_reports"
}

var _ domain.TradeReportRepository = (*GormTradeReportRepository)(nil)

// GormTradeReportRepository implements domain.TradeReportRepository using GORM.
type GormTradeReportRepository struct {
	db *gorm.DB
}

// NewGormTradeReportRepository creates a new instance of GormTradeReportRepository.
func NewGormTradeReportRepository(db *gorm.DB) *GormTradeReportRepository {
	return &GormTradeReportRepository{db: db}
}

// Save executes an idempotent upsert on the trade report using GORM.
func (r *GormTradeReportRepository) Save(ctx context.Context, report *domain.TradeReport) error {
	if r.db == nil {
		return nil // database persistence is disabled
	}

	model := ToGormModel(report)
	err := r.db.WithContext(ctx).Clauses(clause.OnConflict{
		UpdateAll: true,
	}).Create(model).Error

	if err != nil {
		return fmt.Errorf("failed to save reversion trade report: %w", err)
	}
	return nil
}

// ToGormModel maps the domain.TradeReport entity to the GORM persistence model.
func ToGormModel(report *domain.TradeReport) *ReversionTradeReport {
	if report == nil {
		return nil
	}
	return &ReversionTradeReport{
		ReqID:               report.ReqID,
		EventID:             report.EventID,
		Timestamp:           report.Timestamp,
		SettleTime:          report.SettleTime,
		Exchange:            report.Exchange,
		Symbol:              report.Symbol,
		NormalizedSymbol:    report.NormalizedSymbol,
		Side:                report.Side.String(),
		FundingRate:         report.FundingRate,
		CandidateFoundTime:  report.CandidateFoundTime,
		MarginUSDT:          report.MarginUSDT,
		Leverage:            report.Leverage,
		BufferTimeMs:        report.BufferTimeMs,
		LatencyRTTMs:        report.LatencyRTTMs,
		ActualSlippage:      report.ActualSlippage,
		FireOffsetMs:        report.FireOffsetMs,
		IOCOrderID:          report.IOCOrderID,
		IOCOutcome:          report.IOCOutcome,
		IOCReason:           report.IOCReason,
		OrderFilled:         report.OrderFilled,
		FillPrice:           report.FillPrice,
		ClosePrice:          report.ClosePrice,
		VolumeUSDT:          report.VolumeUSDT,
		GrossProfit:         report.GrossProfit,
		NetProfit:           report.NetProfit,
		PnLPct:              report.PnLPct,
		Fee:                 report.Fee,
		HoldFee:             report.HoldFee,
		HoldDurationMs:      report.HoldDurationMs,
		ExitReason:          report.ExitReason,
		CloseRetryCount:     report.CloseRetryCount,
		ForceCloseAttempted: report.ForceCloseAttempted,
		ForceCloseSucceeded: report.ForceCloseSucceeded,
		Status:              report.Status,
		ErrorMsg:            report.ErrorMsg,
	}
}

// InitDatabase initializes the GORM DB connection pool and runs AutoMigrate.
func InitDatabase(lc fx.Lifecycle) (*gorm.DB, error) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return nil, nil // gracefully skip if DATABASE_URL is not set
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	if err := db.AutoMigrate(&ReversionTradeReport{}); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate database: %w", err)
	}

	if lc != nil {
		lc.Append(fx.Hook{
			OnStop: func(ctx context.Context) error {
				sqlDB, err := db.DB()
				if err != nil {
					return fmt.Errorf("failed to get underlying sql.DB: %w", err)
				}
				if err := sqlDB.Close(); err != nil {
					return fmt.Errorf("failed to close sql.DB: %w", err)
				}
				return nil
			},
		})
	}

	return db, nil
}
