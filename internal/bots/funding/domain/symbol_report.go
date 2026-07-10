package domain

import (
	"context"
	"time"
)

// SymbolFundingReport represents the captured funding rate and volume of a symbol before settlement.
type SymbolFundingReport struct {
	ID                       uint      `json:"id"`
	Timestamp                time.Time `json:"timestamp"`
	Exchange                 string    `json:"exchange"`
	Symbol                   string    `json:"symbol"`
	NormalizedSymbol         string    `json:"normalized_symbol"`
	FundingRate              float64   `json:"funding_rate"`
	Volume24h                float64   `json:"volume_24h"`
	SettleTime               time.Time `json:"settle_time"`
	PreFundingPriceFetched   bool      `json:"pre_funding_price_fetched"`
	AfterFundingPriceFetched bool      `json:"after_funding_price_fetched"`
}

// SymbolFundingReportRepository defines the contract for persisting SymbolFundingReports.
type SymbolFundingReportRepository interface {
	SaveBatch(ctx context.Context, reports []SymbolFundingReport) error
	GetPendingPreFunding(ctx context.Context, settleTimeThreshold time.Time) ([]SymbolFundingReport, error)
	GetPendingAfterFunding(ctx context.Context, settleTimeThreshold time.Time) ([]SymbolFundingReport, error)
	UpdatePreFunding(ctx context.Context, id uint, fetched bool) error
	UpdateAfterFunding(ctx context.Context, id uint, fetched bool) error
}

// FundingPriceTick represents the minute-by-minute price snapshot of a symbol before settlement.
type FundingPriceTick struct {
	ID           uint      `json:"id"`
	Exchange     string    `json:"exchange"`
	Symbol       string    `json:"symbol"`
	SettleTime   time.Time `json:"settle_time"`
	Timestamp    time.Time `json:"timestamp"`
	IntervalType string    `json:"interval_type"`
	Price        float64   `json:"price"`
	BidPrice     float64   `json:"bid_price"`
	AskPrice     float64   `json:"ask_price"`
}

// FundingPriceTickRepository defines the contract for persisting and retrieving FundingPriceTicks.
type FundingPriceTickRepository interface {
	SaveBatch(ctx context.Context, ticks []FundingPriceTick) error
	GetTicksForSettle(ctx context.Context, exchange, symbol string, settleTime time.Time) ([]FundingPriceTick, error)
}
