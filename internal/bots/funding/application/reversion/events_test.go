package reversion_test

import (
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/reversion"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestReversionEventsExposeStableMetadata(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	base := reversion.BaseReversionEvent{
		Flow:       reversion.FlowReversion,
		ReqID:      "req-123",
		Symbol:     "BTC_USDT",
		SendNotify: true,
		Timestamp:  now,
		SettleTime: now.Add(time.Hour),
	}
	baseNoNotify := reversion.BaseReversionEvent{
		Flow:       reversion.FlowReversion,
		ReqID:      "req-123",
		Symbol:     "BTC_USDT",
		SendNotify: false,
		Timestamp:  now,
		SettleTime: now.Add(time.Hour),
	}
	candidate := fundingdomain.Candidate{
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001,
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
	}
	ioc := reversion.IOCSubmittedEvent{
		BaseReversionEvent: base,
		Candidate:          candidate,
		OrderID:            "ord-1",
		Side:               shared.SideOpenLong,
		CloseSide:          shared.SideCloseLong,
		IntendedPrice:      60000,
		Volume:             1.25,
		FireTimestamp:      now,
	}

	tests := []struct {
		name        string
		event       reversion.ReversionEvent
		messagePart string
		keys        []string
		notify      bool
	}{
		{
			name:        "candidate",
			event:       reversion.CandidateFoundEvent{BaseReversionEvent: base, Candidate: candidate},
			messagePart: "Candidate found",
			keys:        []string{"symbol", "fundingRate", "side"},
			notify:      true,
		},
		{
			name:        "arm market ready",
			event:       reversion.ArmMarketReadyEvent{BaseReversionEvent: base, Candidate: candidate, BestBid: 59990, BestAsk: 60000, LastPrice: 59995},
			messagePart: "Arm market ready",
			keys:        []string{"symbol", "bestBid", "bestAsk", "lastPrice"},
			notify:      true,
		},
		{
			name:        "arm plan calculated",
			event:       reversion.ArmPlanCalculatedEvent{BaseReversionEvent: base, Candidate: candidate, IOCPrice: 60001, Slippage: 0.02, RequestedVolume: 12.5},
			messagePart: "Arm plan calculated",
			keys:        []string{"symbol", "iocPrice", "slippage", "volume"},
			notify:      true,
		},
		{
			name:        "safety checked",
			event:       reversion.SafetyCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
			messagePart: "Safety checked",
			keys:        []string{"symbol", "passed", "reason", "volume"},
			notify:      true,
		},
		{
			name:        "armed",
			event:       reversion.ArmedEvent{BaseReversionEvent: base, Candidate: candidate, Volume: 12.5, IOCPrice: 60001, Slippage: 0.02},
			messagePart: "Reversion armed",
			keys:        []string{"symbol", "volume", "iocPrice", "slippage"},
			notify:      true,
		},
		{
			name:        "wait complete",
			event:       reversion.WaitCompleteEvent{BaseReversionEvent: base, Candidate: candidate},
			messagePart: "Wait complete",
			keys:        []string{"symbol", "settleTime"},
			notify:      true,
		},
		{
			name:        "confirmed",
			event:       reversion.ConfirmedEvent{BaseReversionEvent: base, FundingRate: 0.001, Candidate: candidate},
			messagePart: "Recheck confirmed",
			keys:        []string{"symbol", "fundingRate"},
			notify:      true,
		},
		{
			name:        "fire timing ready",
			event:       reversion.FireTimingReadyEvent{BaseReversionEvent: base, Candidate: candidate, LatencyRTTMs: 20, FireOffsetMs: 10},
			messagePart: "Fire timing ready",
			keys:        []string{"symbol", "latencyRTTMs", "fireOffsetMs"},
			notify:      true,
		},
		{
			name:        "fire plan checked",
			event:       reversion.FirePlanCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
			messagePart: "Fire plan checked",
			keys:        []string{"symbol", "passed", "reason", "volume"},
			notify:      true,
		},
		{
			name:        "fire window reached",
			event:       reversion.FireWindowReachedEvent{BaseReversionEvent: base, Candidate: candidate, LatencyRTTMs: 20},
			messagePart: "Fire window reached",
			keys:        []string{"symbol", "latencyRTTMs"},
			notify:      true,
		},
		{
			name:        "position watch ready",
			event:       reversion.PositionWatchReadyEvent{BaseReversionEvent: base, Candidate: candidate, Timeout: 10 * time.Second},
			messagePart: "Position watch ready",
			keys:        []string{"symbol", "timeout"},
			notify:      true,
		},
		{
			name:        "ioc submitted",
			event:       ioc,
			messagePart: "IOC order submitted",
			keys:        []string{"symbol", "orderId", "iocPrice", "volume", "error"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: base, IOCEvent: ioc, OrderID: "ord-1", Outcome: reversion.IOCOutcomeFilled, HoldVol: 1.25},
			messagePart: "IOC outcome checked",
			keys:        []string{"symbol", "orderId", "outcome", "holdVol", "reason"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked canceled no fill",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: baseNoNotify, IOCEvent: ioc, OrderID: "ord-1", Outcome: reversion.IOCOutcomeCanceledNoFill, HoldVol: 0},
			messagePart: "IOC order canceled (no fill)",
			keys:        []string{"symbol", "orderId", "outcome", "holdVol", "reason"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked unknown",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: baseNoNotify, IOCEvent: ioc, OrderID: "ord-1", Outcome: reversion.IOCOutcomeUnknown, HoldVol: 0, Reason: "mock-err"},
			messagePart: "IOC order outcome unknown",
			keys:        []string{"symbol", "orderId", "outcome", "holdVol", "reason"},
			notify:      true,
		},
		{
			name:        "filled",
			event:       reversion.OrderFilledEvent{BaseReversionEvent: base, OrderID: "ord-1", Side: shared.SideOpenLong, CloseSide: shared.SideCloseLong, FillPrice: 60010, FillVol: 2, VolumeUSDT: 120020},
			messagePart: "Position filled",
			keys:        []string{"symbol", "orderId", "fillPrice", "fillVol", "volumeUSDT"},
			notify:      true,
		},
		{
			name:        "closed",
			event:       reversion.PositionClosedEvent{BaseReversionEvent: base, EntryPrice: 60000, ClosePrice: 60100, CloseVol: 2, Reason: "target", NetProfit: 10, Fee: -0.5, HoldFee: -0.1},
			messagePart: "Position closed",
			keys:        []string{"symbol", "entryPrice", "closePrice", "closeVol", "reason", "netProfit", "fee", "holdFee"},
			notify:      false,
		},
		{
			name:        "timeout guard scheduled",
			event:       reversion.TimeoutGuardScheduledEvent{BaseReversionEvent: base, IOCEvent: ioc, Timeout: 10 * time.Second},
			messagePart: "Timeout guard scheduled",
			keys:        []string{"symbol", "timeout"},
			notify:      true,
		},
		{
			name:        "timeout position checked",
			event:       reversion.TimeoutPositionCheckedEvent{BaseReversionEvent: base, IOCEvent: ioc, HoldVol: 1.5},
			messagePart: "Timeout position checked",
			keys:        []string{"symbol", "holdVol", "error"},
			notify:      true,
		},
		{
			name:        "force close initiated",
			event:       reversion.ForceCloseInitiatedEvent{BaseReversionEvent: base, IOCEvent: ioc, HoldVol: 1.5, TimeoutSec: 10.0},
			messagePart: "Initiating safety timeout force close",
			keys:        []string{"symbol", "holdVol", "timeout"},
			notify:      true,
		},
		{
			name:        "force close completed",
			event:       reversion.ForceCloseCompletedEvent{BaseReversionEvent: base, IOCEvent: ioc, HoldVol: 1.5, CloseRetryCount: 2, Succeeded: true},
			messagePart: "Force close completed",
			keys:        []string{"symbol", "holdVol", "retries", "succeeded", "error"},
			notify:      true,
		},
		{
			name:        "timeout",
			event:       reversion.TimeoutEvent{BaseReversionEvent: base, Timeout: 3 * time.Second, Reason: "guard", ForceCloseAttempted: true, ForceCloseSucceeded: true, CloseRetryCount: 1},
			messagePart: "Timeout guard triggered",
			keys:        []string{"symbol", "timeout", "reason", "forceCloseSucceeded"},
			notify:      true,
		},
		{
			name:        "abort",
			event:       reversion.AbortEvent{BaseReversionEvent: base, Reason: "not profitable"},
			messagePart: "Cycle aborted",
			keys:        []string{"symbol", "reason"},
			notify:      true,
		},
		{
			name:        "error",
			event:       reversion.ErrorEvent{BaseReversionEvent: base, Error: "boom"},
			messagePart: "Cycle error",
			keys:        []string{"symbol", "error"},
			notify:      true,
		},
		{
			name:        "final pnl",
			event:       reversion.FinalPnLEvent{BaseReversionEvent: base, Direction: shared.SideOpenLong, EntryPrice: 60000, ClosePrice: 60100, NetPnL: 9.4, Fees: -0.5, HoldFee: -0.1},
			messagePart: "Final PnL",
			keys:        nil,
			notify:      true,
		},
		{
			name:        "completed",
			event:       reversion.ReversionCompletedEvent{BaseReversionEvent: base, Reason: "done"},
			messagePart: "Reversion completed",
			keys:        []string{"symbol", "reason"},
			notify:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, reversion.FlowReversion, tt.event.GetFlow())
			assert.Equal(t, "req-123", tt.event.GetReqID())
			assert.Equal(t, "BTC_USDT", tt.event.GetSymbol())
			assert.True(t, strings.Contains(tt.event.GetMessage(), tt.messagePart), tt.event.GetMessage())
			assert.Equal(t, tt.notify, tt.event.ShouldNotify())

			data := tt.event.GetDataMap()
			if data != nil {
				for _, key := range tt.keys {
					assert.Contains(t, data, key)
				}
				assert.Equal(t, "BTC_USDT", data["symbol"])
			} else {
				assert.Empty(t, tt.keys)
			}
		})
	}
}

func TestReversionEventsNotifyOnErrorsEvenWhenSendNotifyFalse(t *testing.T) {
	t.Parallel()

	base := reversion.BaseReversionEvent{Symbol: "ETH_USDT"}
	candidate := fundingdomain.Candidate{TradeIntent: fundingdomain.TradeIntent{Symbol: "ETH_USDT"}}
	ioc := reversion.IOCSubmittedEvent{BaseReversionEvent: base, Candidate: candidate, Error: "exchange rejected"}
	assert.True(t, ioc.ShouldNotify())
	assert.Contains(t, ioc.GetMessage(), "failed")

	timeout := reversion.TimeoutEvent{BaseReversionEvent: base, Error: "close failed"}
	assert.True(t, timeout.ShouldNotify())
	assert.Contains(t, timeout.GetMessage(), "failed to close")

	errEvt := reversion.ErrorEvent{BaseReversionEvent: base, Error: "panic"}
	assert.True(t, errEvt.ShouldNotify())
}

func TestReversionEventColor(t *testing.T) {
	t.Parallel()

	base := reversion.BaseReversionEvent{Symbol: "BTC_USDT"}
	assert.Equal(t, reversion.ColorYellow, base.GetColor())

	// Default event should be yellow
	cand := reversion.CandidateFoundEvent{BaseReversionEvent: base}
	assert.Equal(t, reversion.ColorYellow, cand.GetColor())

	// Custom color on base should be respected
	customBase := reversion.BaseReversionEvent{Symbol: "BTC_USDT", Color: "blue"}
	customCand := reversion.CandidateFoundEvent{BaseReversionEvent: customBase}
	assert.Equal(t, reversion.EventColor("blue"), customCand.GetColor())

	// PositionClosedEvent colors based on NetProfit
	closedPos := reversion.PositionClosedEvent{BaseReversionEvent: base, NetProfit: 10.5}
	assert.Equal(t, reversion.ColorGreen, closedPos.GetColor())

	closedNeg := reversion.PositionClosedEvent{BaseReversionEvent: base, NetProfit: -5.2}
	assert.Equal(t, reversion.ColorRed, closedNeg.GetColor())

	closedZero := reversion.PositionClosedEvent{BaseReversionEvent: base, NetProfit: 0}
	assert.Equal(t, reversion.ColorYellow, closedZero.GetColor())

	// FinalPnLEvent colors based on NetPnL
	pnlPos := reversion.FinalPnLEvent{BaseReversionEvent: base, NetPnL: 10.5}
	assert.Equal(t, reversion.ColorGreen, pnlPos.GetColor())

	pnlNeg := reversion.FinalPnLEvent{BaseReversionEvent: base, NetPnL: -5.2}
	assert.Equal(t, reversion.ColorRed, pnlNeg.GetColor())

	pnlZero := reversion.FinalPnLEvent{BaseReversionEvent: base, NetPnL: 0}
	assert.Equal(t, reversion.ColorYellow, pnlZero.GetColor())

	// IOCOutcomeCheckedEvent colors
	iocOutcomeCanceled := reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: base, Outcome: reversion.IOCOutcomeCanceledNoFill}
	assert.Equal(t, reversion.ColorBlue, iocOutcomeCanceled.GetColor())

	iocOutcomeUnknownNoPos := reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: base, Outcome: reversion.IOCOutcomeUnknown, Reason: reversion.ReversionReason("ioc_outcome_unknown_no_position")}
	assert.Equal(t, reversion.ColorBlue, iocOutcomeUnknownNoPos.GetColor())

	iocOutcomeUnknownOther := reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: base, Outcome: reversion.IOCOutcomeUnknown, Reason: reversion.ReversionReason("other_reason")}
	assert.Equal(t, reversion.ColorRed, iocOutcomeUnknownOther.GetColor())

	// AbortEvent colors
	abortCanceled := reversion.AbortEvent{BaseReversionEvent: base, Reason: reversion.ReversionReason("ioc_canceled_no_position")}
	assert.Equal(t, reversion.ColorBlue, abortCanceled.GetColor())

	abortUnknownNoPos := reversion.AbortEvent{BaseReversionEvent: base, Reason: reversion.ReversionReason("ioc_outcome_unknown_no_position")}
	assert.Equal(t, reversion.ColorBlue, abortUnknownNoPos.GetColor())

	abortOther := reversion.AbortEvent{BaseReversionEvent: base, Reason: reversion.ReversionReason("other_reason")}
	assert.Equal(t, reversion.ColorYellow, abortOther.GetColor())
}

func TestFinalPnLEvent_GetMessage(t *testing.T) {
	t.Parallel()

	base := reversion.BaseReversionEvent{
		Symbol: "BTCUSDT",
		Flow:   "F_12345",
	}

	evt := reversion.FinalPnLEvent{
		BaseReversionEvent: base,
		Direction:          shared.SideOpenShort,
		EntryPrice:         60200.50,
		ClosePrice:         60310.20,
		VolumeUSDT:         12040.00,
		Fees:               0.80,
		HoldFee:            -0.15,
		NetPnL:             12.50,
		PnLPct:             0.18,
		HoldDurationMs:     134000, // 2m 14s
	}

	msg := evt.GetMessage()
	expected := "Final PnL for BTCUSDT of Flow F_12345\n" +
		"• Side: Short\n" +
		"• Price: 60,200.500000 ➔ 60,310.200000 (-0.18%)\n" +
		"• Size: 12,040.00 USDT\n" +
		"• Fees: Exec: $0.8000 | Funding: -$0.1500\n" +
		"• Net PnL: +$12.5000 (+0.18%) [2m 14s]"

	assert.Equal(t, expected, msg)
}
