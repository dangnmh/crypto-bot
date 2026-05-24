package cycle

import (
	"testing"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/notifier"

	"github.com/stretchr/testify/require"
)

func TestRuntimeBuildNotificationMessageFormatsKnownTopics(t *testing.T) {
	t.Parallel()

	rt := &Runtime{cfg: config.SymbolConfig{Symbol: "BTC_USDT", MarginUSDT: 3}}

	tests := []struct {
		name        string
		topic       string
		payload     any
		wantLevel   notifier.Level
		wantMessage string
	}{
		{
			name:        "final pnl value",
			topic:       events.TopicCycleFinalPnL,
			payload:     events.FinalPnLEvent{TotalPnL: 0.022, TradingFees: -0.0145, FundingFeePaid: 0, NetPnL: 0.0074, EventCount: 7},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "💰 Cycle Completed\nClosing PNL (USDT): 0.022\nTrading Fee (USDT): -0.0145\nFunding Fees (USDT): 0\nPNL Rate: +0.25%\nNet PnL: 0.0074 USDT",
		},
		{
			name:        "final pnl pointer",
			topic:       events.TopicCycleFinalPnL,
			payload:     &events.FinalPnLEvent{TotalPnL: -1.5, TradingFees: 0.5, FundingFeePaid: 0, NetPnL: -2, EventCount: 3},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "💰 Cycle Completed\nClosing PNL (USDT): -1.5\nTrading Fee (USDT): -0.5\nFunding Fees (USDT): 0\nPNL Rate: -66.67%\nNet PnL: -2 USDT",
		},
		{
			name:        "order filled",
			topic:       events.TopicReversionOrderFilled,
			payload:     events.OrderFilledEvent{FillPrice: 100.12345, FillVol: 2.5, Profit: 0.33},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "✅ Order Filled\nPrice: 100.1235\nVol: 2.5000\nProfit: 0.3300",
		},
		{
			name:        "trap filled pointer",
			topic:       events.TopicTrapOrderFilled,
			payload:     &events.OrderFilledEvent{FillPrice: 99, FillVol: 1, Profit: -0.1},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "✅ Order Filled\nPrice: 99.0000\nVol: 1.0000\nProfit: -0.1000",
		},
		{
			name:        "abort",
			topic:       events.TopicTrapAbort,
			payload:     events.CycleAbortEvent{Reason: "spread too wide"},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "⚠️ Cycle Aborted\nReason: spread too wide",
		},
		{
			name:        "abort pointer",
			topic:       events.TopicScanAbort,
			payload:     &events.CycleAbortEvent{Reason: "no candidate"},
			wantLevel:   notifier.LevelTrading,
			wantMessage: "⚠️ Cycle Aborted\nReason: no candidate",
		},
		{
			name:        "error",
			topic:       events.TopicReversionError,
			payload:     events.CycleErrorEvent{Error: "exchange down"},
			wantLevel:   notifier.LevelCritical,
			wantMessage: "❌ Cycle Error\nError: exchange down",
		},
		{
			name:        "symbol disabled",
			topic:       events.TopicSymbolDisabled,
			payload:     &events.SymbolDisabledEvent{Reason: "rejected", Source: "risk"},
			wantLevel:   notifier.LevelCritical,
			wantMessage: "🚫 Symbol Disabled\nReason: rejected\nSource: risk",
		},
		{
			name:        "order rejected",
			topic:       events.TopicOrderRejected,
			payload:     events.WSOrderRejectedEvent{Error: "insufficient margin"},
			wantLevel:   notifier.LevelCritical,
			wantMessage: "🚨 Order Rejected\nError: insufficient margin",
		},
		{
			name:        "order rejected pointer",
			topic:       events.TopicOrderRejected,
			payload:     &events.WSOrderRejectedEvent{Error: "invalid price"},
			wantLevel:   notifier.LevelCritical,
			wantMessage: "🚨 Order Rejected\nError: invalid price",
		},
		{
			name:        "default",
			topic:       "funding.custom",
			payload:     struct{}{},
			wantLevel:   notifier.LevelInfo,
			wantMessage: "Event: funding.custom",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := rt.buildNotificationMessage(tt.topic, tt.payload)
			require.Equal(t, "BTC_USDT", got.Symbol)
			require.Equal(t, tt.wantLevel, got.Level)
			require.Equal(t, tt.wantMessage, got.Message)
			require.False(t, got.Timestamp.IsZero())
		})
	}
}

func TestRuntimeShouldNotify(t *testing.T) {
	t.Parallel()

	rt := &Runtime{}

	require.True(t, rt.shouldNotify("custom", events.BaseEvent{SendNotify: true}))
	require.True(t, rt.shouldNotify(events.TopicOrderRejected, struct{}{}))
	require.False(t, rt.shouldNotify("custom", events.BaseEvent{}))
	require.False(t, rt.shouldNotify("custom", struct{}{}))
}
