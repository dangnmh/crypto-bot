package exchange_test

import (
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
)

func TestExternalOrderID(t *testing.T) {
	t.Parallel()

	settleTime := time.Date(2026, 7, 7, 15, 30, 0, 0, time.UTC)

	// Test other exchanges default limit
	gateID := exchange.ExternalOrderID("JST-USDT-SWAP", settleTime, "gate")
	assert.True(t, len(gateID) <= 28)

	orangexID := exchange.ExternalOrderID("JST-USDT-SWAP", settleTime, "orangex")
	assert.True(t, len(orangexID) <= 30)

	binanceID := exchange.ExternalOrderID("JST-USDT-SWAP", settleTime, "binance")
	assert.True(t, len(binanceID) <= 32)

	deepcoinID := exchange.ExternalOrderID("JST-USDT-SWAP", settleTime, "deepcoin")
	assert.True(t, len(deepcoinID) <= 20)
}
