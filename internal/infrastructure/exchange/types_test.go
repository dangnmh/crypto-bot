package exchange

import (
	"testing"
)

func TestSideStr(t *testing.T) {
	tests := []struct {
		side     int
		expected string
	}{
		{SideOpenLong, "LONG"},
		{SideOpenShort, "SHORT"},
		{SideCloseShort, "CLOSE_SHORT"},
		{SideCloseLong, "CLOSE_LONG"},
		{999, "UNKNOWN"},
	}

	for _, tt := range tests {
		actual := SideStr(tt.side)
		if actual != tt.expected {
			t.Errorf("SideStr(%d): expected %s, got %s", tt.side, tt.expected, actual)
		}
	}
}

func TestCloseSideFor(t *testing.T) {
	if CloseSideFor(SideOpenLong) != SideCloseLong {
		t.Errorf("CloseSideFor(SideOpenLong) should be SideCloseLong")
	}
	if CloseSideFor(SideOpenShort) != SideCloseShort {
		t.Errorf("CloseSideFor(SideOpenShort) should be SideCloseShort")
	}
}

func TestIsTerminalOrderState(t *testing.T) {
	terminalStates := []int{OrderStateFilled, OrderStateCanceled, OrderStatePartial}
	for _, state := range terminalStates {
		if !IsTerminalOrderState(state) {
			t.Errorf("expected state %d to be terminal", state)
		}
	}

	nonTerminalStates := []int{1, 2, 6}
	for _, state := range nonTerminalStates {
		if IsTerminalOrderState(state) {
			t.Errorf("expected state %d to NOT be terminal", state)
		}
	}
}
