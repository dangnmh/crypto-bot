package reversion_test

import (
	"context"
	"encoding/json"
	"log/slog"
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
		Flow:       reversion.FlowIDFundingReversion,
		ReqID:      "req-123",
		Symbol:     "BTC_USDT",
		Timestamp:  now,
		SettleTime: now.Add(time.Hour),
		OrderID:    "ord-123",
		ExternalID: "ext-123",
	}

	candidate := fundingdomain.Candidate{
		Symbol:      "BTC_USDT",
		FundingRate: 0.001,
		Side:        shared.SideOpenLong,
		CloseSide:   shared.SideCloseLong,
		BestBid:     59990,
		BestAsk:     60000,
		LastPrice:   59995,
		Volume:      12.5,
		Slippage:    0.02,
	}

	tests := []struct {
		name  string
		event reversion.ReversionEvent
	}{
		{
			name:  "candidate",
			event: reversion.CandidateFoundEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "arm market ready",
			event: reversion.ArmMarketReadyEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "arm plan calculated",
			event: reversion.ArmPlanCalculatedEvent{BaseReversionEvent: base, Candidate: candidate, IOCPrice: 60001},
		},
		{
			name:  "safety checked",
			event: reversion.SafetyCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
		},
		{
			name:  "armed",
			event: reversion.ArmedEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "wait complete",
			event: reversion.WaitCompleteEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "confirmed",
			event: reversion.ConfirmedEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "margin mode ready",
			event: reversion.MarginModeReadyEvent{BaseReversionEvent: base, Candidate: candidate},
		},
		{
			name:  "fire timing ready",
			event: reversion.FireTimingReadyEvent{BaseReversionEvent: base, Candidate: candidate, LatencyRTTMs: 20, FireOffsetMs: 10},
		},
		{
			name:  "fire plan checked",
			event: reversion.FirePlanCheckedEvent{BaseReversionEvent: base, Candidate: candidate, AdjustedVolume: 12.5, Passed: true},
		},
		{
			name:  "abort",
			event: reversion.AbortEvent{BaseReversionEvent: base, Reason: "not profitable"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, reversion.FlowIDFundingReversion, tt.event.GetFlow())
			assert.Equal(t, "req-123", tt.event.GetReqID())
			assert.Equal(t, "BTC_USDT", tt.event.GetSymbol())
			assert.Equal(t, "ord-123", tt.event.GetOrderID())
			assert.Equal(t, "ext-123", tt.event.GetExternalID())
		})
	}
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
		ExternalID: "client_id_123",
		Topic:      "funding.reversion.candidate",
	}
	assert.Equal(t, "client_id_123funding.reversion.candidate", evt.DeduplicateKey())
}

func TestReversionEvents_EventBusDeduplication(t *testing.T) {
	t.Parallel()

	bus := eventbus.New(slog.Default())
	defer func() { _ = bus.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	topic := reversion.TopicReversionArmed
	msgs, err := bus.Subscribe(ctx, topic)
	require.NoError(t, err)

	base := reversion.BaseReversionEvent{
		ExternalID: "client_id_dup",
		Topic:      topic,
	}

	evt1 := reversion.ArmedEvent{
		BaseReversionEvent: base,
		Candidate:          fundingdomain.Candidate{Symbol: "BTC_USDT", Volume: 1},
	}
	evt2 := reversion.ArmedEvent{
		BaseReversionEvent: base,
		Candidate:          fundingdomain.Candidate{Symbol: "BTC_USDT", Volume: 2},
	}

	// Publish first event
	err = bus.Publish(topic, evt1)
	require.NoError(t, err)

	// Publish second event (duplicate)
	err = bus.Publish(topic, evt2)
	require.NoError(t, err)

	// We should receive exactly 1 message
	var received []float64
	select {
	case msg := <-msgs:
		var rec reversion.ArmedEvent
		require.NoError(t, json.Unmarshal(msg.Payload, &rec))
		received = append(received, rec.Candidate.Volume)
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
	assert.Equal(t, 1.0, received[0])
}
