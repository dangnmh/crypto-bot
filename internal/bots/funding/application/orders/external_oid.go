package orders

import (
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// ExternalUniqueID delegates to exchange.ExternalUniqueID.
func ExternalUniqueID(symbol string, settleTime time.Time, exchangeName string) string {
	return exchange.ExternalUniqueID(symbol, settleTime, exchangeName)
}

// ExternalOrderID delegates to exchange.ExternalOrderID.
func ExternalOrderID(symbol string, settleTime time.Time, exchangeName string) string {
	return exchange.ExternalOrderID(symbol, settleTime, exchangeName)
}
