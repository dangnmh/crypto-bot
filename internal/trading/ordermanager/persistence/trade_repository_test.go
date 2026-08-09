package persistence_test

import (
	"context"
	"testing"

	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/internal/trading/ordermanager/persistence"
)

func TestGormTradeRepository_NilDBHandling(t *testing.T) {
	t.Parallel()
	repo := persistence.NewGormTradeRepository(nil)

	ctx := context.Background()

	record := ordermanager.OrderTradeRecordEvent{
		BaseExecutionEvent: ordermanager.BaseExecutionEvent{
			ReqID:        "req-100",
			Symbol:       "BTCUSDT",
			Exchange:     "MEXC",
			StrategyType: "PENNY_JUMPER",
		},
		Side:   "LONG",
		NetPnL: 990.0,
	}

	// Saving with nil DB should gracefully return nil
	if err := repo.Save(ctx, record); err != nil {
		t.Errorf("expected no error on nil DB, got %v", err)
	}

	tRecord := &persistence.TradeRecord{}
	if tRecord.TableName() != "trades" {
		t.Errorf("expected TableName to be trades, got %s", tRecord.TableName())
	}
}
