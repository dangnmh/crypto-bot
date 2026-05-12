package exchange_test

import (
	"testing"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/stretchr/testify/assert"
)

func TestSideStr(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		side     int
		expected string
	}{
		{"long", exchange.SideOpenLong, "LONG"},
		{"short", exchange.SideOpenShort, "SHORT"},
		{"close short", exchange.SideCloseShort, "CLOSE_SHORT"},
		{"close long", exchange.SideCloseLong, "CLOSE_LONG"},
		{"unknown", 999, "UNKNOWN"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, exchange.SideStr(tt.side))
		})
	}
}

func TestCloseSideFor(t *testing.T) {
	t.Parallel()
	assert.Equal(t, exchange.SideCloseLong, exchange.CloseSideFor(exchange.SideOpenLong))
	assert.Equal(t, exchange.SideCloseShort, exchange.CloseSideFor(exchange.SideOpenShort))
}

func TestIsTerminalOrderState(t *testing.T) {
	t.Parallel()
	terminalStates := []int{exchange.OrderStateFilled, exchange.OrderStateCanceled, exchange.OrderStatePartial}
	for _, state := range terminalStates {
		assert.True(t, exchange.IsTerminalOrderState(state))
	}

	nonTerminalStates := []int{1, 2, 6}
	for _, state := range nonTerminalStates {
		assert.False(t, exchange.IsTerminalOrderState(state))
	}
}
