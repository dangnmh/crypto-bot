package reversion_test

import (
	"testing"

	"crypto-bot/internal/bots/funding/application/reversion"

	"github.com/stretchr/testify/assert"
)

func TestGetNormalizedSymbol(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected string
	}{
		{"BTC-USDT", "BTC"},
		{"ETH_USDT", "ETH"},
		{"SOLUSDT", "SOL"},
		{"TAIKO-USDT-SWAP", "TAIKO"},
		{"TAIKO-USDT-PERPETUAL", "TAIKO"},
		{"TAIKO-SWAP-USDT", "TAIKO"},
		{"H-USDT", "HOME"},
		{"BTC-USD", "BTC"},
		{"ETH_USD", "ETH"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, reversion.GetNormalizedSymbol(tt.input))
		})
	}
}
