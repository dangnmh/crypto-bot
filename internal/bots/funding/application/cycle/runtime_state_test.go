package cycle_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRuntimeCalculatesFinalPnLFromRecordedEvents(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicReversionOrderFilled, events.OrderFilledEvent{
		Flow:   events.FlowReversion,
		Symbol: "BTC_USDT",
		Profit: 10,
		Fee:    0.5,
	})
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicTrapOrderFilled, events.OrderFilledEvent{
		Flow:   events.FlowTrap,
		Symbol: "BTC_USDT",
		Profit: 4,
		Fee:    0.25,
	})

	got := rt.CalculateFinalPnL()

	assert.Equal(t, "BTC_USDT", got.Symbol)
	assert.InDelta(t, 14, got.TotalPnL, 1e-9)
	assert.InDelta(t, 10, got.IocPnL, 1e-9)
	assert.InDelta(t, 4, got.TrapPnL, 1e-9)
	assert.InDelta(t, 0.75, got.TradingFees, 1e-9)
	assert.InDelta(t, 13.25, got.NetPnL, 1e-9)
}

func TestRuntimeCalculatesFinalPnLFromClosedPositionWhenPresent(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicReversionOrderFilled, events.OrderFilledEvent{
		Flow:    events.FlowReversion,
		Symbol:  "BTC_USDT",
		OrderID: "order-1",
		Profit:  0,
		Fee:     0.0087054,
	})
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicReversionOrderFilled, events.OrderFilledEvent{
		Flow:    events.FlowReversion,
		Symbol:  "BTC_USDT",
		OrderID: "order-1",
		Profit:  0,
		Fee:     0.0087054,
	})
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicReversionPositionClosed, events.PositionClosedEvent{
		Flow:       events.FlowReversion,
		Symbol:     "BTC_USDT",
		ClosePrice: 0.1317,
		CloseVol:   11,
		Profit:     0.022,
		NetProfit:  0.0074,
		Fee:        -0.0145,
		Method:     "ws_position",
	})

	got := rt.CalculateFinalPnL()

	assert.InDelta(t, 0.022, got.TotalPnL, 1e-9)
	assert.InDelta(t, 0.022, got.IocPnL, 1e-9)
	assert.InDelta(t, -0.0145, got.TradingFees, 1e-9)
	assert.InDelta(t, 0.0074, got.NetPnL, 1e-9)
	assert.InDelta(t, 0.1317, got.ClosePrice, 1e-9)
}

func TestRuntimeRecordAndPublishAnnotatesFlowAndSettle(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicTrapSkipped, events.TrapSkippedEvent{
		Flow:   events.FlowTrap,
		Symbol: "BTC_USDT",
		Reason: "disabled",
	})

	log := rt.JourneyEvents()
	require.Len(t, log, 2)
	assert.Equal(t, events.TopicTrapSkipped, log[1].Topic)
	assert.Equal(t, events.FlowTrap, log[1].Flow)
	assert.Equal(t, rt.SettleTime(), log[1].SettleTime)

	log[1].Topic = "mutated"
	assert.Equal(t, events.TopicTrapSkipped, rt.JourneyEvents()[1].Topic)
}

func TestRuntimePublishFinalPnLIncludesJourneySnapshot(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicReversionOrderFilled, events.OrderFilledEvent{
		Flow:   events.FlowReversion,
		Symbol: "BTC_USDT",
		Profit: 10,
		Fee:    0.5,
	})
	rt.RecordAndPublish(context.Background(), "req-1", events.TopicCycleCompleted, events.CycleCompletedEvent{
		Reason: "trailing",
	})

	rt.PublishFinalPnL(context.Background(), "req-1")

	log := rt.JourneyEvents()
	require.Len(t, log, 4)
	finalEnv := log[3]
	require.Equal(t, events.TopicCycleFinalPnL, finalEnv.Topic)

	var final events.FinalPnLEvent
	require.NoError(t, json.Unmarshal(finalEnv.Payload, &final))
	assert.Equal(t, "BTC_USDT", final.Symbol)
	assert.InDelta(t, 9.5, final.NetPnL, 1e-9)
	assert.Equal(t, 3, final.EventCount)
	require.Len(t, final.Journey, 3)
	assert.Equal(t, events.TopicCycleStarted, final.Journey[0].Topic)
	assert.Equal(t, events.TopicReversionOrderFilled, final.Journey[1].Topic)
	assert.Equal(t, events.TopicCycleCompleted, final.Journey[2].Topic)
}

func TestRuntimeTrapSnapshotReturnsCopies(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)
	order := events.TrapFiredEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
		Side:    shared.SideOpenShort,
		Volume:  3,
	}
	fill := events.OrderFilledEvent{
		Flow:    events.FlowTrap,
		Symbol:  "BTC_USDT",
		OrderID: "trap-1",
		FillVol: 3,
	}

	rt.MarkTrapOrder(order)
	rt.MarkTrapFill(fill)
	order.OrderID = "mutated"
	fill.FillVol = 99

	snapshotOrder, hasOrder, snapshotFill, hasFill, terminal := rt.TrapSnapshot()
	require.True(t, hasOrder)
	require.True(t, hasFill)
	assert.False(t, terminal)
	assert.Equal(t, "trap-1", snapshotOrder.OrderID)
	assert.InDelta(t, 3, snapshotFill.FillVol, 1e-9)

	snapshotOrder.OrderID = "changed"
	snapshotFill.FillVol = 42
	againOrder, againHasOrder, againFill, againHasFill, againTerminal := rt.TrapSnapshot()
	require.True(t, againHasOrder)
	require.True(t, againHasFill)
	assert.False(t, againTerminal)
	assert.Equal(t, "trap-1", againOrder.OrderID)
	assert.InDelta(t, 3, againFill.FillVol, 1e-9)

	rt.MarkTrapTerminal()
	terminalOrder, terminalHasOrder, terminalFill, terminalHasFill, terminal := rt.TrapSnapshot()
	require.True(t, terminalHasOrder)
	require.True(t, terminalHasFill)
	assert.Equal(t, "trap-1", terminalOrder.OrderID)
	assert.InDelta(t, 3, terminalFill.FillVol, 1e-9)
	assert.True(t, terminal)
}

func TestRuntimeFlowTerminalIsIdempotent(t *testing.T) {
	t.Parallel()

	rt := newCycleRuntimeForTest(t)

	assert.True(t, rt.TryMarkFlowTerminal(events.FlowReversion))
	assert.False(t, rt.TryMarkFlowTerminal(events.FlowReversion))
	assert.True(t, rt.IsFlowTerminal(events.FlowReversion))
	assert.False(t, rt.IsFlowTerminal(events.FlowTrap))
}

func newCycleRuntimeForTest(t *testing.T) *cycle.Runtime {
	t.Helper()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	settle := time.Date(2026, 5, 20, 12, 0, 0, 0, time.UTC)
	rt := cycle.NewRuntime(config.SymbolConfig{Symbol: "BTC_USDT"}, &config.Config{}, cycle.Deps{
		Log: logger,
	})
	rt.Begin(context.Background(), "req-1", settle, logger)
	rt.SetCandidate(fundingdomain.Candidate{
		Config: cycle.ToTradeConfig(config.SymbolConfig{Symbol: "BTC_USDT"}),
	})
	t.Cleanup(func() {
		require.NoError(t, rt.CloseBus())
	})
	return rt
}
