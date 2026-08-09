package ordermanager_test

import (
	"testing"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/trading/ordermanager"
)

func TestOrderExecutionAggregate_ApplyAndReplay(t *testing.T) {
	t.Parallel()
	agg := ordermanager.NewOrderExecutionAggregate("req-999")

	if agg.State() != ordermanager.StateInit {
		t.Errorf("expected initial state StateInit, got %s", agg.State())
	}

	evt1 := ordermanager.OrderIntentEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:        "req-999",
			Symbol:       "BTCUSDT",
			Exchange:     "MEXC",
			StrategyType: ordermanager.StrategyFundingReversion,
			Timestamp:    time.Now(),
		},
		Side:      shared.SideOpenLong,
		OrderType: ordermanager.OrderTypeIOC,
		Price:     50000.0,
		Volume:    1.0,
	}

	evt2 := ordermanager.OrderPreFlightCompletedEvent{
		OrderIntentEvent: evt1,
		AdjustedLeverage: 10,
	}

	evt3 := ordermanager.OrderSubmittedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:     "req-999",
			Symbol:    "BTCUSDT",
			Timestamp: time.Now(),
		},
		Price:  50000.0,
		Volume: 1.0,
	}

	evt4 := ordermanager.OrderOutcomeResolvedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:     "req-999",
			Symbol:    "BTCUSDT",
			Timestamp: time.Now(),
		},
		Outcome:   ordermanager.OutcomeFilled,
		FilledVol: 1.0,
		AvgPrice:  50000.0,
	}

	evt5 := ordermanager.OrderCompletedEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:     "req-999",
			Symbol:    "BTCUSDT",
			Timestamp: time.Now(),
		},
		Outcome:    "filled",
		EntryPrice: 50000.0,
		ExitPrice:  51000.0,
		Volume:     1.0,
		NetProfit:  1000.0,
	}

	events := []ordermanager.OrderEvent{evt1, evt2, evt3, evt4, evt5}

	for _, e := range events {
		if err := agg.Record(e); err != nil {
			t.Fatalf("failed to record event: %v", err)
		}
	}

	if agg.State() != ordermanager.StateCompleted {
		t.Errorf("expected final state StateCompleted, got %s", agg.State())
	}

	tradeRecord := agg.BuildTradeRecord()
	expectedKey := "req-999-ordermanager.trade_record"
	if tradeRecord.DeduplicateKey() != expectedKey {
		t.Errorf("expected DeduplicateKey %s, got %s", expectedKey, tradeRecord.DeduplicateKey())
	}

	if len(agg.UncommittedEvents()) != 5 {
		t.Errorf("expected 5 uncommitted events, got %d", len(agg.UncommittedEvents()))
	}

	// Test Replay
	replayed := ordermanager.NewOrderExecutionAggregate("req-999")
	if err := replayed.Replay(events); err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if replayed.State() != ordermanager.StateCompleted {
		t.Errorf("expected replayed state StateCompleted, got %s", replayed.State())
	}
	if replayed.Version() != 5 {
		t.Errorf("expected replayed version 5, got %d", replayed.Version())
	}
}
