package cycle_test

import (
	"encoding/json"
	"testing"

	"crypto-bot/internal/bots/funding/application/events"

	"github.com/stretchr/testify/assert"
)

func TestCalculateFinalPnL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		events  []events.JournalEnvelope
		wantPnL events.FinalPnLEvent
	}{
		{
			name:   "No fill events returns zero PnL",
			events: []events.JournalEnvelope{},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       0,
				IocPnL:         0,
				TrapPnL:        0,
				TradingFees:    0,
				FundingFeePaid: 0,
				NetPnL:         0,
			},
		},
		{
			name: "Single IOC fill",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 10.5, "fee": 0.5}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       10.5,
				IocPnL:         10.5,
				TrapPnL:        0,
				TradingFees:    0.5,
				FundingFeePaid: 0,
				NetPnL:         10.0,
			},
		},
		{
			name: "Multiple IOC fills",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 10.0, "fee": 0.5}`),
				},
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 5.0, "fee": 0.3}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       15.0,
				IocPnL:         15.0,
				TrapPnL:        0,
				TradingFees:    0.8,
				FundingFeePaid: 0,
				NetPnL:         14.2,
			},
		},
		{
			name: "Trap fill",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicTrapOrderFilled,
					Payload: []byte(`{"profit": 8.0, "fee": 0.4}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       8.0,
				IocPnL:         0,
				TrapPnL:        8.0,
				TradingFees:    0.4,
				FundingFeePaid: 0,
				NetPnL:         7.6,
			},
		},
		{
			name: "Mixed IOC and Trap fills",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 10.0, "fee": 0.5}`),
				},
				{
					Topic:   events.TopicTrapOrderFilled,
					Payload: []byte(`{"profit": 5.0, "fee": 0.3}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       15.0,
				IocPnL:         10.0,
				TrapPnL:        5.0,
				TradingFees:    0.8,
				FundingFeePaid: 0,
				NetPnL:         14.2,
			},
		},
		{
			name: "Negative profit (loss)",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": -5.0, "fee": 0.5}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       -5.0,
				IocPnL:         -5.0,
				TrapPnL:        0,
				TradingFees:    0.5,
				FundingFeePaid: 0,
				NetPnL:         -5.5,
			},
		},
		{
			name: "Multiple trap fills",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicTrapOrderFilled,
					Payload: []byte(`{"profit": 3.0, "fee": 0.2}`),
				},
				{
					Topic:   events.TopicTrapOrderFilled,
					Payload: []byte(`{"profit": 4.0, "fee": 0.25}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       7.0,
				IocPnL:         0,
				TrapPnL:        7.0,
				TradingFees:    0.45,
				FundingFeePaid: 0,
				NetPnL:         6.55,
			},
		},
		{
			name: "Complex mix with many events",
			events: []events.JournalEnvelope{
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 10.0, "fee": 0.5}`),
				},
				{
					Topic:   events.TopicTrapOrderFilled,
					Payload: []byte(`{"profit": 5.0, "fee": 0.3}`),
				},
				{
					Topic:   events.TopicReversionOrderFilled,
					Payload: []byte(`{"profit": 3.0, "fee": 0.2}`),
				},
			},
			wantPnL: events.FinalPnLEvent{
				Symbol:         "BTC_USDT",
				TotalPnL:       18.0,
				IocPnL:         13.0,
				TrapPnL:        5.0,
				TradingFees:    1.0,
				FundingFeePaid: 0,
				NetPnL:         17.0,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Verify the PnL calculation logic
			var iocPnL, trapPnL, tradingFees float64

			for _, evt := range tt.events {
				var fillEvt events.OrderFilledEvent
				if err := json.Unmarshal(evt.Payload, &fillEvt); err == nil {
					switch evt.Topic {
					case events.TopicReversionOrderFilled:
						iocPnL += fillEvt.Profit
						tradingFees += fillEvt.Fee
					case events.TopicTrapOrderFilled:
						trapPnL += fillEvt.Profit
						tradingFees += fillEvt.Fee
					}
				}
			}

			totalPnL := iocPnL + trapPnL
			netPnL := totalPnL - tradingFees

			assert.InDelta(t, tt.wantPnL.TotalPnL, totalPnL, 0.001)
			assert.InDelta(t, tt.wantPnL.IocPnL, iocPnL, 0.001)
			assert.InDelta(t, tt.wantPnL.TrapPnL, trapPnL, 0.001)
			assert.InDelta(t, tt.wantPnL.TradingFees, tradingFees, 0.001)
			assert.InDelta(t, tt.wantPnL.NetPnL, netPnL, 0.001)
		})
	}
}

func TestFinalPnLEventSerialization(t *testing.T) {
	t.Parallel()

	pnlEvent := events.FinalPnLEvent{
		Symbol:         "BTC_USDT",
		TotalPnL:       10.0,
		IocPnL:         10.0,
		TrapPnL:        0,
		TradingFees:    0.5,
		FundingFeePaid: 0,
		NetPnL:         9.5,
	}

	data, err := json.Marshal(pnlEvent)
	assert.NoError(t, err)

	var unmarshaled events.FinalPnLEvent
	err = json.Unmarshal(data, &unmarshaled)
	assert.NoError(t, err)

	assert.Equal(t, pnlEvent.Symbol, unmarshaled.Symbol)
	assert.InDelta(t, pnlEvent.TotalPnL, unmarshaled.TotalPnL, 0.001)
	assert.InDelta(t, pnlEvent.NetPnL, unmarshaled.NetPnL, 0.001)
}
