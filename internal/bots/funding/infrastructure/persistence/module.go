package persistence

import (
	"crypto-bot/internal/bots/funding/domain"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// Module wires funding bot persistence repositories.
var Module = fx.Options(
	fx.Provide(
		ProvideSymbolFundingReportRepository,
		ProvideFundingPriceTickRepository,
	),
)

// ProvideSymbolFundingReportRepository provides the GORM-backed SymbolFundingReportRepository.
func ProvideSymbolFundingReportRepository(db *gorm.DB) domain.SymbolFundingReportRepository {
	return NewGormSymbolFundingReportRepository(db)
}

// ProvideFundingPriceTickRepository provides the GORM-backed FundingPriceTickRepository.
func ProvideFundingPriceTickRepository(db *gorm.DB) domain.FundingPriceTickRepository {
	return NewGormFundingPriceTickRepository(db)
}
