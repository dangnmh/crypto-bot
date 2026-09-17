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

func TestMaxClientOrderIDLength(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 20, exchange.MaxClientOrderIDLength("deepcoin"))
	assert.Equal(t, 20, exchange.GetMaxClientOrderIDLength("deepcoin_futures"))
	assert.Equal(t, 28, exchange.MaxClientOrderIDLength("gate"))
	assert.Equal(t, 28, exchange.GetMaxClientOrderIDLength("gate_futures"))
	assert.Equal(t, 30, exchange.MaxClientOrderIDLength("orangex"))
	assert.Equal(t, 30, exchange.GetMaxClientOrderIDLength("orangex_futures"))
	assert.Equal(t, 32, exchange.MaxClientOrderIDLength("mexc"))
	assert.Equal(t, 32, exchange.GetMaxClientOrderIDLength("mexc_futures"))
	assert.Equal(t, 32, exchange.MaxClientOrderIDLength("binance"))
	assert.Equal(t, 32, exchange.GetMaxClientOrderIDLength("unknown"))
}

func TestGenerateClientOrderID(t *testing.T) {
	t.Parallel()

	for _, ex := range []string{"deepcoin", "gate", "orangex", "binance", "mexc"} {
		oid := exchange.GenerateClientOrderID(ex)
		maxLen := exchange.MaxClientOrderIDLength(ex)
		assert.Len(t, oid, maxLen)
		for _, r := range oid {
			assert.True(t, (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9'), "must be alphanumeric: %c", r)
		}
	}
}
