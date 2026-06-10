package domain_test

import (
	"encoding/json"
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
		{"zero", domain.SideUnknown, "UNKNOWN"},
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

func TestSide_Opposite(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		give domain.Side
		want domain.Side
	}{
		{"open long", domain.SideOpenLong, domain.SideOpenShort},
		{"open short", domain.SideOpenShort, domain.SideOpenLong},
		{"close long", domain.SideCloseLong, domain.SideCloseShort},
		{"close short", domain.SideCloseShort, domain.SideCloseLong},
		{"unknown", domain.Side(99), domain.Side(99)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, tt.give.Opposite())
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

func TestSide_MarshalJSON(t *testing.T) {
	t.Parallel()

	type payload struct {
		Side domain.Side `json:"side"`
	}

	data, err := json.Marshal(payload{Side: domain.SideOpenLong})
	assert.NoError(t, err)
	assert.JSONEq(t, `{"side":"LONG"}`, string(data))
}

func TestSide_MarshalJSON_Invalid(t *testing.T) {
	t.Parallel()

	_, err := json.Marshal(domain.Side(99))
	assert.Error(t, err)
}

func TestSide_UnmarshalJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		json string
		want domain.Side
	}{
		{"long label", `"LONG"`, domain.SideOpenLong},
		{"open long alias", `"OPEN_LONG"`, domain.SideOpenLong},
		{"short label", `"SHORT"`, domain.SideOpenShort},
		{"open short alias", `"OPEN_SHORT"`, domain.SideOpenShort},
		{"close short label", `"CLOSE_SHORT"`, domain.SideCloseShort},
		{"close long label", `"CLOSE_LONG"`, domain.SideCloseLong},
		{"case insensitive", `"close_long"`, domain.SideCloseLong},
		{"unknown label", `"UNKNOWN"`, domain.SideUnknown},
		{"zero", `0`, domain.SideUnknown},
		{"number", `3`, domain.SideOpenShort},
		{"numeric string", `"4"`, domain.SideCloseLong},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got domain.Side
			err := json.Unmarshal([]byte(tt.json), &got)
			assert.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSide_UnmarshalJSON_Invalid(t *testing.T) {
	t.Parallel()

	tests := []string{`"BAD"`, `99`, `""`, `{}`}
	for _, raw := range tests {
		t.Run(raw, func(t *testing.T) {
			t.Parallel()

			var got domain.Side
			err := json.Unmarshal([]byte(raw), &got)
			assert.Error(t, err)
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
		state domain.OrderState
		want  bool
	}{
		{"filled", domain.OrderStateFilled, true},
		{"canceled", domain.OrderStateCanceled, true},
		{"partial canceled", domain.OrderStatePartial, true},
		{"new", domain.OrderState(0), false},
		{"partially filled", domain.OrderState(1), false},
		{"untriggered", domain.OrderState(6), false},
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
	assert.Equal(t, domain.OrderType(1), domain.OrderTypeLimit)
	assert.Equal(t, domain.OrderType(2), domain.OrderTypePostOnly)
	assert.Equal(t, domain.OrderType(3), domain.OrderTypeIOC)
	assert.Equal(t, domain.OrderType(4), domain.OrderTypeFOK)
	assert.Equal(t, domain.OrderType(5), domain.OrderTypeMarket)
}

func TestOpenTypeConstants(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.OpenType(1), domain.OpenTypeIsolated)
	assert.Equal(t, domain.OpenType(2), domain.OpenTypeCross)
}

func TestIntervalMin1(t *testing.T) {
	t.Parallel()
	assert.Equal(t, domain.Interval("Min1"), domain.IntervalMin1)
}
