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

	records, err := repo.GetProfitableTradeRecords(ctx, "MEXC", 10.0, record.Timestamp)
	if err != nil || len(records) != 0 {
		t.Errorf("expected empty records on nil DB, got %v, err=%v", records, err)
	}

	if err := repo.MarkObfuscated(ctx, "req-100", record.Timestamp); err != nil {
		t.Errorf("expected no error on MarkObfuscated with nil DB, got %v", err)
	}

	tRecord := &persistence.TradeRecord{
		Extra: map[string]any{"source": "test", "val": 123},
	}
	if tRecord.TableName() != "trades" {
		t.Errorf("expected TableName to be trades, got %s", tRecord.TableName())
	}
	if tRecord.Extra["source"] != "test" {
		t.Errorf("expected Extra[source] to be test, got %v", tRecord.Extra["source"])
	}
}
