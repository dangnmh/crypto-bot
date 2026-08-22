package persistence_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/trading/ordermanager"
	"crypto-bot/internal/trading/ordermanager/persistence"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGormTradeRepository_NilDBHandling(t *testing.T) {
	t.Parallel()
	repo := persistence.NewGormTradeRepository(nil)

	ctx := context.Background()

	record := ordermanager.OrderTradeRecordEvent{
		ReqID:        "req-100",
		Symbol:       "BTCUSDT",
		Exchange:     "MEXC",
		StrategyType: "PENNY_JUMPER",
		Side:         "LONG",
		NetPnL:       990.0,
	}

	// Saving with nil DB should gracefully return nil
	if err := repo.Save(ctx, record); err != nil {
		t.Errorf("expected no error on nil DB, got %v", err)
	}

	summaries, err := repo.GetSymbolPnLSummaries(ctx, "MEXC", record.Timestamp)
	if err != nil || len(summaries) != 0 {
		t.Errorf("expected empty summaries on nil DB, got %v, err=%v", summaries, err)
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

func TestGormTradeRepository_GetSymbolPnLSummaries_SQLite(t *testing.T) {
	t.Parallel()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&persistence.TradeRecord{}); err != nil {
		t.Fatalf("failed to auto-migrate: %v", err)
	}

	repo := persistence.NewGormTradeRepository(db)
	ctx := context.Background()
	now := time.Now()

	// 1. Insert funding profit trades for COW (+200 USD) and LPT (+50 USD)
	require.NoError(t, repo.Save(ctx, ordermanager.OrderTradeRecordEvent{
		ReqID:        "f-cow-1",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: ordermanager.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       150.0,
		RecordedAt:   now.Add(-1 * time.Hour),
	}))
	require.NoError(t, repo.Save(ctx, ordermanager.OrderTradeRecordEvent{
		ReqID:        "f-cow-2",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: ordermanager.StrategyFundingArbitrage,
		Side:         "SHORT",
		NetPnL:       50.0,
		RecordedAt:   now.Add(-30 * time.Minute),
	}))
	require.NoError(t, repo.Save(ctx, ordermanager.OrderTradeRecordEvent{
		ReqID:        "f-lpt-1",
		Symbol:       "LPT-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: ordermanager.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       50.0,
		RecordedAt:   now.Add(-20 * time.Minute),
	}))

	// 2. Insert Obfuscator trade for COW (-30 USD)
	require.NoError(t, repo.Save(ctx, ordermanager.OrderTradeRecordEvent{
		ReqID:        "obf-cow-1",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: ordermanager.StrategyObfuscator,
		Side:         "LONG",
		NetPnL:       -30.0,
		RecordedAt:   now.Add(-10 * time.Minute),
	}))

	// 3. Query summaries
	since := now.Add(-24 * time.Hour)
	summaries, err := repo.GetSymbolPnLSummaries(ctx, "toobit_futures", since)
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	// Verify results are sorted by funding_net_profit descending
	assert.Equal(t, "COW-SWAP-USDT", summaries[0].Symbol)
	assert.Equal(t, 200.0, summaries[0].FundingNetProfit)
	assert.Equal(t, -30.0, summaries[0].ObfuscatorNetPnL)

	assert.Equal(t, "LPT-SWAP-USDT", summaries[1].Symbol)
	assert.Equal(t, 50.0, summaries[1].FundingNetProfit)
	assert.Equal(t, 0.0, summaries[1].ObfuscatorNetPnL)
}
