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
	}
	candidate := fundingdomain.Candidate{
		TradeIntent: fundingdomain.TradeIntent{
			Symbol:      "BTC_USDT",
			FundingRate: 0.001,
			Side:        shared.SideOpenLong,
			CloseSide:   shared.SideCloseLong,
		},
	}

	tests := []struct {
		name        string
		event       reversion.ReversionEvent
		messagePart string
		keys        []string
		notify      bool
	}{
		{
			name: "candidate",
			event: reversion.CandidateFoundEvent{
				BaseReversionEvent: base,
				Candidate:          candidate,
				SettleTime:         now.Add(time.Hour),
			},
			messagePart: "Candidate found",
			keys:        []string{"symbol", "fundingRate", "side"},
			notify:      true,
		},
		{
			name: "armed",
			event: reversion.ArmedEvent{
				BaseReversionEvent: base,
				Candidate:          candidate,
				Volume:             12.5,
				IOCPrice:           60001,
				Slippage:           0.02,
				SettleTime:         now.Add(time.Hour),
			},
			messagePart: "Reversion armed",
			keys:        []string{"symbol", "volume", "iocPrice", "slippage"},
			notify:      true,
		},
		{
			name: "wait complete",
			event: reversion.WaitCompleteEvent{
				BaseReversionEvent: base,
				SettleTime:         now.Add(time.Hour),
				Candidate:          candidate,
			},
			messagePart: "Wait complete",
			keys:        []string{"symbol", "settleTime"},
			notify:      true,
		},
		{
			name: "confirmed",
			event: reversion.ConfirmedEvent{
				BaseReversionEvent: base,
				FundingRate:        0.001,
				Candidate:          candidate,
				SettleTime:         now.Add(time.Hour),
			},
			messagePart: "Recheck confirmed",
			keys:        []string{"symbol", "fundingRate"},
			notify:      true,
		},
		{
			name: "ioc fired",
			event: reversion.IOCFiredEvent{
				BaseReversionEvent: base,
				OrderID:            "ord-1",
				Side:               shared.SideOpenLong,
				CloseSide:          shared.SideCloseLong,
				IntendedPrice:      60000,
				Volume:             1.25,
				FireTimestamp:      now,
			},
			messagePart: "IOC Order fired",
			keys:        []string{"symbol", "orderId", "intendedPrice", "volume", "error"},
			notify:      true,
		},
		{
			name: "filled",
			event: reversion.OrderFilledEvent{
				BaseReversionEvent: base,
				OrderID:            "ord-1",
				Side:               shared.SideOpenLong,
				CloseSide:          shared.SideCloseLong,
				FillPrice:          60010,
				FillVol:            2,
			},
			messagePart: "Position FILLED",
			keys:        []string{"symbol", "orderId", "fillPrice", "fillVol"},
			notify:      true,
		},
		{
			name: "closed",
			event: reversion.PositionClosedEvent{
				BaseReversionEvent: base,
				EntryPrice:         60000,
				ClosePrice:         60100,
				CloseVol:           2,
				Reason:             "target",
				NetProfit:          10,
				Fee:                -0.5,
				HoldFee:            -0.1,
			},
			messagePart: "Position CLOSED",
			keys:        []string{"symbol", "entryPrice", "closePrice", "closeVol", "reason", "netProfit", "fee", "holdFee"},
			notify:      true,
		},
		{
			name: "timeout",
			event: reversion.TimeoutEvent{
				BaseReversionEvent:  base,
				Timeout:             3 * time.Second,
				Reason:              "guard",
				ForceCloseAttempted: true,
				ForceCloseSucceeded: true,
				CloseRetryCount:     1,
			},
			messagePart: "Timeout Guard TRIGGERED",
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
			name: "final pnl",
			event: reversion.FinalPnLEvent{
				BaseReversionEvent: base,
				Direction:          shared.SideOpenLong,
				EntryPrice:         60000,
				ClosePrice:         60100,
				NetPnL:             9.4,
				Fees:               -0.5,
				HoldFee:            -0.1,
			},
			messagePart: "Final PnL",
			keys:        []string{"symbol", "entryPrice", "closePrice", "netPnL", "fee", "holdFee"},
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
			for _, key := range tt.keys {
				assert.Contains(t, data, key)
			}
			assert.Equal(t, "BTC_USDT", data["symbol"])
		})
	}
}

func TestReversionEventsNotifyOnErrorsEvenWhenSendNotifyFalse(t *testing.T) {
	t.Parallel()

	base := reversion.BaseReversionEvent{Symbol: "ETH_USDT"}

	ioc := reversion.IOCFiredEvent{BaseReversionEvent: base, Error: "exchange rejected"}
	assert.True(t, ioc.ShouldNotify())
	assert.Contains(t, ioc.GetMessage(), "FAILED")

	timeout := reversion.TimeoutEvent{BaseReversionEvent: base, Error: "close failed"}
	assert.True(t, timeout.ShouldNotify())
	assert.Contains(t, timeout.GetMessage(), "failed to close")

	errEvt := reversion.ErrorEvent{BaseReversionEvent: base, Error: "panic"}
	assert.True(t, errEvt.ShouldNotify())
}
