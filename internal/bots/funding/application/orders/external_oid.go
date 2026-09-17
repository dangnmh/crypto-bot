package orders

import (
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// GenerateClientOrderID delegates to exchange.GenerateClientOrderID.
func GenerateClientOrderID(exchangeName string) string {
	return exchange.GenerateClientOrderID(exchangeName)
}

// ExternalUniqueID delegates to exchange.ExternalUniqueID.
func ExternalUniqueID(symbol string, settleTime time.Time, exchangeName string) string {
	return exchange.ExternalUniqueID(symbol, settleTime, exchangeName)
}

// ExternalOrderID delegates to exchange.ExternalOrderID.
func ExternalOrderID(symbol string, settleTime time.Time, exchangeName string) string {
	return exchange.ExternalOrderID(symbol, settleTime, exchangeName)
}

// MaxClientOrderIDLength delegates to exchange.MaxClientOrderIDLength.
func MaxClientOrderIDLength(exchangeName string) int {
	return exchange.MaxClientOrderIDLength(exchangeName)
}

// GetMaxClientOrderIDLength delegates to exchange.GetMaxClientOrderIDLength.
func GetMaxClientOrderIDLength(exchangeName string) int {
	return exchange.GetMaxClientOrderIDLength(exchangeName)
}
