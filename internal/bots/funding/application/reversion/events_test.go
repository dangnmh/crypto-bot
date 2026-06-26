package reversion_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/reversion"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/eventbus"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	baseWithOrderAndSide := base
	baseWithOrderAndSide.OrderID = "ord-1"
	baseWithOrderAndSide.Side = shared.SideOpenLong

	baseWithOrderAndSideNoNotify := baseNoNotify
	baseWithOrderAndSideNoNotify.OrderID = "ord-1"
	baseWithOrderAndSideNoNotify.Side = shared.SideOpenLong

	candidate := fundingdomain.Candidate{
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001,
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
		MarketData: fundingdomain.MarketData{
			BestBid:   59990,
			BestAsk:   60000,
			LastPrice: 59995,
		},
		TradePlan: fundingdomain.TradePlan{
			Volume:   12.5,
			Slippage: 0.02,
		},
	}

	ioc := reversion.IOCSubmittedEvent{
		BaseReversionEvent: baseWithOrderAndSide,
		Candidate:          candidate,
		IntendedPrice:      60000,
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
			keys:        []string{"fundingRate", "side"},
			notify:      true,
		},
		{
			name:        "arm market ready",
			event:       reversion.ArmMarketReadyEvent{BaseReversionEvent: base, Candidate: candidate},
			messagePart: "Arm market ready",
			keys:        []string{"bestBid", "bestAsk", "lastPrice"},
			notify:      true,
		},
		{
			name:        "arm plan calculated",
			event:       reversion.ArmPlanCalculatedEvent{BaseReversionEvent: base, Candidate: candidate, IOCPrice: 60001},
			messagePart: "Arm plan calculated",
			keys:        []string{"iocPrice", "slippage", "volume"},
			notify:      true,
		},
		{
			name:        "safety checked",
			event:       reversion.SafetyCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
			messagePart: "Safety checked",
			keys:        []string{"passed", "volume"},
			notify:      true,
		},
		{
			name:        "armed",
			event:       reversion.ArmedEvent{BaseReversionEvent: base, Candidate: candidate},
			messagePart: "Armed",
			keys:        []string{"volume", "iocPrice", "slippage"},
			notify:      true,
		},
		{
			name:        "wait complete",
			event:       reversion.WaitCompleteEvent{BaseReversionEvent: base, Candidate: candidate},
			messagePart: "Wait complete",
			keys:        []string{"settleTime"},
			notify:      true,
		},
		{
			name:        "confirmed",
			event:       reversion.ConfirmedEvent{BaseReversionEvent: base, FundingRate: 0.001, Candidate: candidate},
			messagePart: "Recheck confirmed",
			keys:        []string{"fundingRate"},
			notify:      true,
		},
		{
			name:        "fire timing ready",
			event:       reversion.FireTimingReadyEvent{BaseReversionEvent: base, Candidate: candidate, LatencyRTTMs: 20, FireOffsetMs: 10},
			messagePart: "Fire timing ready",
			keys:        []string{"latencyRTTMs", "fireOffsetMs"},
			notify:      true,
		},
		{
			name:        "fire plan checked",
			event:       reversion.FirePlanCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
			messagePart: "Fire plan checked",
			keys:        []string{"passed", "volume"},
			notify:      true,
		},
		{
			name:        "fire window reached",
			event:       reversion.FireWindowReachedEvent{BaseReversionEvent: base, Candidate: candidate, LatencyRTTMs: 20},
			messagePart: "Fire window reached",
			keys:        []string{"latencyRTTMs"},
			notify:      true,
		},
		{
			name:        "position watch ready",
			event:       reversion.PositionWatchReadyEvent{BaseReversionEvent: base, Candidate: candidate, Timeout: 10 * time.Second},
			messagePart: "Position watch ready",
			keys:        []string{"timeout"},
			notify:      true,
		},
		{
			name:        "ioc submitted",
			event:       ioc,
			messagePart: "IOC order submitted",
			keys:        []string{"orderId", "iocPrice", "volume", "fundingRate", "volusdt24h"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: baseWithOrderAndSide, Outcome: reversion.IOCOutcomeFilled, HoldVol: 1.25, FundingRate: 0.001, VolUSDT24h: 60_000_000},
			messagePart: "IOC outcome checked",
			keys:        []string{"orderId", "outcome", "holdVol", "fundingRate", "volusdt24h"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked canceled no fill",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: baseWithOrderAndSideNoNotify, Outcome: reversion.IOCOutcomeCanceledNoFill, HoldVol: 0, FundingRate: 0.001, VolUSDT24h: 60_000_000},
			messagePart: "IOC order canceled (no fill)",
			keys:        []string{"orderId", "outcome", "holdVol", "fundingRate", "volusdt24h"},
			notify:      true,
		},
		{
			name:        "ioc outcome checked unknown",
			event:       reversion.IOCOutcomeCheckedEvent{BaseReversionEvent: baseWithOrderAndSideNoNotify, Outcome: reversion.IOCOutcomeUnknown, HoldVol: 0, Reason: "mock-err", FundingRate: 0.001, VolUSDT24h: 60_000_000},
			messagePart: "IOC outcome unknown",
			keys:        []string{"orderId", "outcome", "holdVol", "reason", "fundingRate", "volusdt24h"},
			notify:      true,
		},
		{
			name:        "filled",
			event:       reversion.OrderFilledEvent{BaseReversionEvent: baseWithOrderAndSide, FillPrice: 60010, FillVol: 2, VolumeUSDT: 120020},
			messagePart: "Position filled",
			keys:        []string{"orderId", "fillPrice", "fillVol", "volumeUSDT"},
			notify:      true,
		},
		{
			name:        "closed",
			event:       reversion.PositionClosedEvent{BaseReversionEvent: base, EntryPrice: 60000, ClosePrice: 60100, CloseVol: 2, Reason: "target", NetProfit: 10, Fee: -0.5, HoldFee: -0.1},
			messagePart: "Position closed",
			keys:        []string{"entryPrice", "closePrice", "closeVol", "reason", "netProfit", "fee", "holdFee"},
			notify:      false,
		},
		{
			name:        "timeout guard scheduled",
			event:       reversion.TimeoutGuardScheduledEvent{BaseReversionEvent: base, Timeout: 10 * time.Second},
			messagePart: "Timeout guard scheduled",
			keys:        []string{"timeout"},
			notify:      true,
		},
		{
			name:        "timeout position checked",
			event:       reversion.TimeoutPositionCheckedEvent{BaseReversionEvent: base, HoldVol: 1.5},
			messagePart: "Timeout position checked",
			keys:        []string{"holdVol"},
			notify:      true,
		},
		{
			name:        "force close initiated",
			event:       reversion.ForceCloseInitiatedEvent{BaseReversionEvent: base, HoldVol: 1.5, TimeoutSec: 10.0},
			messagePart: "CRITICAL: Safety timeout close initiated",
			keys:        []string{"holdVol", "timeout"},
			notify:      true,
		},
		{
			name:        "force close completed",
			event:       reversion.ForceCloseCompletedEvent{BaseReversionEvent: base, HoldVol: 1.5, CloseRetryCount: 2, Succeeded: true},
			messagePart: "Force close completed",
			keys:        []string{"holdVol", "retries", "succeeded"},
			notify:      true,
		},
		{
			name:        "timeout",
			event:       reversion.TimeoutEvent{BaseReversionEvent: base, Timeout: 3 * time.Second, Reason: "guard", ForceCloseAttempted: true, ForceCloseSucceeded: true, CloseRetryCount: 1},
			messagePart: "Timeout guard triggered",
			keys:        []string{"timeout", "reason", "forceCloseSucceeded"},
			notify:      true,
		},
		{
			name:        "abort",
			event:       reversion.AbortEvent{BaseReversionEvent: base, Reason: "not profitable"},
			messagePart: "Cycle aborted",
			keys:        []string{"reason"},
			notify:      true,
		},
		{
			name:        "error",
			event:       reversion.ErrorEvent{BaseReversionEvent: base, Error: "boom"},
			messagePart: "Cycle error",
			keys:        []string{"error"},
			notify:      true,
		},
		{
			name:        "final pnl",
			event:       reversion.FinalPnLEvent{BaseReversionEvent: baseWithOrderAndSide, EntryPrice: 60000, ClosePrice: 60100, NetPnL: 9.4, Fees: -0.5, HoldFee: -0.1},
			messagePart: "PnL",
			keys:        nil,
			notify:      true,
		},
		{
			name:        "completed",
			event:       reversion.ReversionCompletedEvent{BaseReversionEvent: base, Reason: "done"},
			messagePart: "Reversion completed",
			keys:        []string{"reason"},
			notify:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, reversion.FlowReversion, tt.event.GetFlow())
			assert.Equal(t, "req-123", tt.event.GetReqID())
			assert.Equal(t, "BTC_USDT", tt.event.GetSymbol())
			assert.True(t, strings.Contains(tt.event.GetMessage(), tt.messagePart), "expected %q to contain %q", tt.event.GetMessage(), tt.messagePart)
			assert.Equal(t, tt.notify, tt.event.ShouldNotify())

			data := tt.event.GetDataMap()
			if data != nil {
				for _, key := range tt.keys {
					assert.Contains(t, data, key)
				}
				// Verify symbol is NOT in the data map to prevent duplicate transmission
				assert.NotContains(t, data, "symbol")
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
	assert.Contains(t, timeout.GetMessage(), "failed")

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
		Symbol:      "BTCUSDT",
		Flow:        "F_12345",
		Side:        shared.SideOpenShort,
		OrderID:     "ord-12345",
		ExternalID:  "client-abc",
		FundingRate: -0.002,
	}

	evt := reversion.FinalPnLEvent{
		BaseReversionEvent: base,
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
	// Let's assert against the new compact layout
	assert.Contains(t, msg, "PnL: +$12.5000 (+0.18%)")
	assert.Contains(t, msg, "Side: Short")
	assert.Contains(t, msg, "Price: 60,200.500000 ➔ 60,310.200000")
	assert.Contains(t, msg, "FR: -0.2%")
	assert.Contains(t, msg, "Order ID: ord-12345")
	assert.Contains(t, msg, "Client ID: client-abc")
}

func TestBaseReversionEvent_DeduplicateKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		externalID string
		topic      string
		expected   string
	}{
		{
			name:       "Both fields set",
			externalID: "client_id_123",
			topic:      "funding.reversion.armed",
			expected:   "client_id_123funding.reversion.armed",
		},
		{
			name:       "Empty externalID",
			externalID: "",
			topic:      "funding.reversion.armed",
			expected:   "",
		},
		{
			name:       "Empty topic",
			externalID: "client_id_123",
			topic:      "",
			expected:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			evt := reversion.BaseReversionEvent{
				ExternalID: tt.externalID,
				Topic:      tt.topic,
			}
			assert.Equal(t, tt.expected, evt.DeduplicateKey())
		})
	}
}

func TestCandidateFoundEvent_DeduplicateKeyInherited(t *testing.T) {
	t.Parallel()

	evt := reversion.CandidateFoundEvent{
		BaseReversionEvent: reversion.BaseReversionEvent{
			ExternalID: "client_id_123",
			Topic:      "funding.reversion.candidate",
		},
	}
	assert.Equal(t, "client_id_123funding.reversion.candidate", evt.DeduplicateKey())
}

func TestReversionEvents_EventBusDeduplication(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	topic := "funding.reversion.position_closed"
	msgs, err := bus.Subscribe(ctx, topic)
	require.NoError(t, err)

	base := reversion.BaseReversionEvent{
		ExternalID: "client_id_dup",
		Topic:      topic,
	}

	evt1 := reversion.PositionClosedEvent{
		BaseReversionEvent: base,
		Reason:             "first_publish",
	}
	evt2 := reversion.PositionClosedEvent{
		BaseReversionEvent: base,
		Reason:             "second_publish",
	}

	// Publish first event
	err = bus.Publish(topic, evt1)
	require.NoError(t, err)

	// Publish second event (duplicate)
	err = bus.Publish(topic, evt2)
	require.NoError(t, err)

	// We should receive exactly 1 message
	var received []string
	select {
	case msg := <-msgs:
		var rec reversion.PositionClosedEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &rec))
		received = append(received, rec.Reason)
		msg.Ack()
	case <-ctx.Done():
		t.Fatal("timeout waiting for first message")
	}

	// Wait a bit to ensure no second message comes
	select {
	case msg := <-msgs:
		t.Fatalf("received unexpected duplicate message: %s", string(msg.Payload))
	case <-time.After(100 * time.Millisecond):
		// Success: no second message arrived
	}

	assert.Len(t, received, 1)
	assert.Equal(t, "first_publish", received[0])
}
