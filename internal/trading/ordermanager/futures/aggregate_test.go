package futures_test

import (
	"testing"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/trading/ordermanager/futures"
)

func TestOrderExecutionAggregate_ApplyAndReplay(t *testing.T) {
	t.Parallel()
	agg := futures.NewOrderExecutionAggregate("req-999")

	if agg.State() != futures.StateInit {
		t.Errorf("expected initial state StateInit, got %s", agg.State())
	}

	evt1 := futures.OrderIntentEvent{
		ReqID:        "req-999",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: futures.StrategyFundingReversion,
		Timestamp:    time.Now(),
		Side:         shared.SideOpenLong,
		OrderType:    futures.OrderTypeIOC,
		Price:        50000.0,
		Volume:       1.0,
		FireTime:     time.Unix(1700000000, 0),
	}

	evt2 := futures.OrderPreFlightCompletedEvent{
		OrderIntentEvent: evt1,
		AdjustedLeverage: 10,
	}

	evt3 := futures.OrderSubmittedEvent{
		OrderPreFlightCompletedEvent: evt2,
		Price:                        50000.0,
		Volume:                       1.0,
	}

	evt4 := futures.OrderOutcomeResolvedEvent{
		ReqID:     "req-999",
		Symbol:    "BTCUSDT",
		Timestamp: time.Now(),
		Outcome:   futures.OutcomeFilled,
		FilledVol: 1.0,
		AvgPrice:  50000.0,
	}

	evt5 := futures.OrderCompletedEvent{
		ReqID:            "req-999",
		Symbol:           "BTCUSDT",
		Timestamp:        time.Now(),
		Outcome:          "filled",
		EntryPrice:       50000.0,
		ExitPrice:        51000.0,
		CloseVolContract: 1.0,
		NetProfit:        1000.0,
	}

	events := []futures.OrderEvent{evt1, evt2, evt3, evt4, evt5}

	for _, e := range events {
		if err := agg.Record(e); err != nil {
			t.Fatalf("failed to record event: %v", err)
		}
	}

	if agg.State() != futures.StateCompleted {
		t.Errorf("expected final state StateCompleted, got %s", agg.State())
	}

	tradeRecord := agg.BuildTradeRecord()
	expectedKey := "req-999-ordermanager.trade_record"
	if tradeRecord.DeduplicateKey() != expectedKey {
		t.Errorf("expected DeduplicateKey %s, got %s", expectedKey, tradeRecord.DeduplicateKey())
	}

	if tradeRecord.FireAt == nil || !tradeRecord.FireAt.Equal(time.Unix(1700000000, 0)) {
		t.Errorf("expected FireAt to be 1700000000, got %v", tradeRecord.FireAt)
	}

	if len(agg.UncommittedEvents()) != 5 {
		t.Errorf("expected 5 uncommitted events, got %d", len(agg.UncommittedEvents()))
	}

	// Test Replay
	replayed := futures.NewOrderExecutionAggregate("req-999")
	if err := replayed.Replay(events); err != nil {
		t.Fatalf("Replay failed: %v", err)
	}

	if replayed.State() != futures.StateCompleted {
		t.Errorf("expected replayed state StateCompleted, got %s", replayed.State())
	}
	if replayed.Version() != 5 {
		t.Errorf("expected replayed version 5, got %d", replayed.Version())
	}
}

func TestOrderExecutionAggregate_SettleTime(t *testing.T) {
	t.Parallel()

	settleTime := time.Date(2026, 8, 13, 22, 0, 0, 0, time.UTC)

	agg := futures.NewOrderExecutionAggregate("req-settle-1")
	evt1 := futures.OrderIntentEvent{
		ReqID:        "req-settle-1",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: futures.StrategyFundingReversion,
		Timestamp:    time.Now(),
		Side:         shared.SideOpenLong,
		SettleTime:   &settleTime,
	}

	_ = agg.Record(evt1)
	record := agg.BuildTradeRecord()

	if record.SettleTime == nil {
		t.Fatalf("expected SettleTime to be non-nil")
	}
	if !record.SettleTime.Equal(settleTime) {
		t.Errorf("expected SettleTime %v, got %v", settleTime, record.SettleTime)
	}

	// Test nil SettleTime
	aggNil := futures.NewOrderExecutionAggregate("req-settle-nil")
	evtNil := futures.OrderIntentEvent{
		ReqID:        "req-settle-nil",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: futures.StrategyFundingReversion,
		Timestamp:    time.Now(),
		Side:         shared.SideOpenLong,
	}
	_ = aggNil.Record(evtNil)
	recordNil := aggNil.BuildTradeRecord()

	if recordNil.SettleTime != nil {
		t.Errorf("expected nil SettleTime, got %v", recordNil.SettleTime)
	}
}

func TestOrderExecutionAggregate_Concurrency(t *testing.T) {
	t.Parallel()

	agg := futures.NewOrderExecutionAggregate("req-concurrent-test")
	const numGoroutines = 50

	done := make(chan struct{})
	for i := range numGoroutines {
		go func(idx int) {
			defer func() { done <- struct{}{} }()

			evt := futures.OrderIntentEvent{
				ReqID:        "req-concurrent-test",
				Symbol:       "BTCUSDT",
				Exchange:     "bybit",
				StrategyType: futures.StrategyFundingReversion,
				Timestamp:    time.Now(),
				Side:         shared.SideOpenLong,
				OrderType:    futures.OrderTypeIOC,
				Price:        50000.0,
				Volume:       float64(idx + 1),
			}

			_ = agg.Record(evt)
			_ = agg.ReqID()
			_ = agg.ClientOrderID()
			_ = agg.Symbol()
			_ = agg.Exchange()
			_ = agg.MarketType()
			_ = agg.StrategyType()
			_ = agg.Side()
			_ = agg.ContractSize()
			_ = agg.FillVolContract()
			_ = agg.FillVolCoin()
			_ = agg.State()
			_ = agg.Version()
			_ = agg.UncommittedEvents()
			_ = agg.HasSubmitted()
			_ = agg.HasFilled()
			_ = agg.BuildTradeRecord()
		}(i)
	}

	for range numGoroutines {
		<-done
	}

	if len(agg.UncommittedEvents()) != numGoroutines {
		t.Errorf("expected %d uncommitted events, got %d", numGoroutines, len(agg.UncommittedEvents()))
	}
}
