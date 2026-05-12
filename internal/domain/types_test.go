package domain_test

import (
	"testing"

	"crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────────────────────────────────────────────────
// Side tests
// ──────────────────────────────────────────────────────────────────────.

func TestSide_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		side domain.Side
		want string
	}{
		{"open long", domain.SideOpenLong, "LONG"},
		{"open short", domain.SideOpenShort, "SHORT"},
		{"close short", domain.SideCloseShort, "CLOSE_SHORT"},
		{"close long", domain.SideCloseLong, "CLOSE_LONG"},
		{"unknown", domain.Side(99), "UNKNOWN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.side.String())
		})
	}
}

func TestSide_IsLong(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		side domain.Side
		want bool
	}{
		{"open long", domain.SideOpenLong, true},
		{"close long", domain.SideCloseLong, true},
		{"open short", domain.SideOpenShort, false},
		{"close short", domain.SideCloseShort, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.side.IsLong())
		})
	}
}

func TestCloseSideFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		open domain.Side
		want domain.Side
	}{
		{"long", domain.SideOpenLong, domain.SideCloseLong},
		{"short", domain.SideOpenShort, domain.SideCloseShort},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.CloseSideFor(tt.open))
		})
	}
}

// ──────────────────────────────────────────────────────────────────────
// OrderState tests
// ──────────────────────────────────────────────────────────────────────.

func TestIsTerminalOrderState(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		state int
		want  bool
	}{
		{"filled", domain.OrderStateFilled, true},
		{"canceled", domain.OrderStateCanceled, true},
		{"partial canceled", domain.OrderStatePartial, true},
		{"new", 0, false},
		{"partially filled", 1, false},
		{"untriggered", 6, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, domain.IsTerminalOrderState(tt.state))
		})
	}
}

// ──────────────────────────────────────────────────────────────────────
// Constant value tests (guard against accidental changes)
// ──────────────────────────────────────────────────────────────────────.

func TestOrderTypeConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, domain.OrderTypeLimit)
	assert.Equal(t, 2, domain.OrderTypePostOnly)
	assert.Equal(t, 3, domain.OrderTypeIOC)
	assert.Equal(t, 4, domain.OrderTypeFOK)
	assert.Equal(t, 5, domain.OrderTypeMarket)
}

func TestOpenTypeConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 1, domain.OpenTypeIsolated)
	assert.Equal(t, 2, domain.OpenTypeCross)
}

func TestIntervalMin1(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "Min1", domain.IntervalMin1)
}
