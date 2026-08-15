package persistence

import (
	"crypto-bot/internal/bots/funding/domain"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// Module wires funding bot persistence repositories.
var Module = fx.Options(
	fx.Provide(
		ProvideTradeReportRepository,
		ProvideSymbolFundingReportRepository,
		ProvideFundingPriceTickRepository,
	),
)

// ProvideTradeReportRepository provides the GORM-backed TradeReportRepository.
func ProvideTradeReportRepository(db *gorm.DB) domain.TradeReportRepository {
	return NewGormTradeReportRepository(db)
}

// ProvideSymbolFundingReportRepository provides the GORM-backed SymbolFundingReportRepository.
func ProvideSymbolFundingReportRepository(db *gorm.DB) domain.SymbolFundingReportRepository {
	return NewGormSymbolFundingReportRepository(db)
}

// ProvideFundingPriceTickRepository provides the GORM-backed FundingPriceTickRepository.
func ProvideFundingPriceTickRepository(db *gorm.DB) domain.FundingPriceTickRepository {
	return NewGormFundingPriceTickRepository(db)
}
