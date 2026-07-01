package domain

import (
	"context"
	"time"
)

// SymbolFundingReport represents the captured funding rate and volume of a symbol before settlement.
type SymbolFundingReport struct {
	ID               uint      `json:"id"`
	Timestamp        time.Time `json:"timestamp"`
	Exchange         string    `json:"exchange"`
	Symbol           string    `json:"symbol"`
	NormalizedSymbol string    `json:"normalized_symbol"`
	FundingRate      float64   `json:"funding_rate"`
	Volume24h        float64   `json:"volume_24h"`
	SettleTime       time.Time `json:"settle_time"`
}

// SymbolFundingReportRepository defines the contract for persisting SymbolFundingReports.
type SymbolFundingReportRepository interface {
	SaveBatch(ctx context.Context, reports []SymbolFundingReport) error
}
