package persistence

import (
	"crypto-bot/internal/trading/ordermanager"

	"go.uber.org/fx"
	"gorm.io/gorm"
)

// Module wires ordermanager persistence repositories.
var Module = fx.Options(
	fx.Provide(
		ProvideTradeRepository,
	),
)

// ProvideTradeRepository provides the GORM-backed TradeRepository.
func ProvideTradeRepository(db *gorm.DB) ordermanager.TradeRepository {
	return NewGormTradeRepository(db)
}
