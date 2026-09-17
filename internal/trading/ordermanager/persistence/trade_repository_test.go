package persistence_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/trading/ordermanager/common"
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

	record := common.OrderTradeRecordEvent{
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
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "f-cow-1",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: common.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       150.0,
		RecordedAt:   now.Add(-1 * time.Hour),
	}))
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "f-cow-2",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: common.StrategyFundingArbitrage,
		Side:         "SHORT",
		NetPnL:       50.0,
		RecordedAt:   now.Add(-30 * time.Minute),
	}))
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "f-lpt-1",
		Symbol:       "LPT-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: common.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       50.0,
		RecordedAt:   now.Add(-20 * time.Minute),
	}))

	// 2. Insert Obfuscator trade for COW (-30 USD)
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "obf-cow-1",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "toobit_futures",
		StrategyType: common.StrategyObfuscator,
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

func TestGormTradeRepository_GetAccountSymbolPnLSummaries(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open("file:mem_acc_pnl?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&persistence.TradeRecord{}))

	repo := persistence.NewGormTradeRepository(db)
	ctx := context.Background()
	now := time.Now()

	// Account 1 trade
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "acc1-cow-1",
		AccountID:    "mexc_main",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "mexc",
		StrategyType: common.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       100.0,
		RecordedAt:   now.Add(-1 * time.Hour),
	}))

	// Account 2 trade
	require.NoError(t, repo.Save(ctx, common.OrderTradeRecordEvent{
		ReqID:        "acc2-cow-1",
		AccountID:    "mexc_sub1",
		Symbol:       "COW-SWAP-USDT",
		Exchange:     "mexc",
		StrategyType: common.StrategyFundingReversion,
		Side:         "LONG",
		NetPnL:       50.0,
		RecordedAt:   now.Add(-30 * time.Minute),
	}))

	since := now.Add(-24 * time.Hour)

	// Query for Account 1
	sums1, err := repo.GetAccountSymbolPnLSummaries(ctx, "mexc_main", "mexc", since)
	require.NoError(t, err)
	require.Len(t, sums1, 1)
	assert.Equal(t, 100.0, sums1[0].FundingNetProfit)

	// Query for Account 2
	sums2, err := repo.GetAccountSymbolPnLSummaries(ctx, "mexc_sub1", "mexc", since)
	require.NoError(t, err)
	require.Len(t, sums2, 1)
	assert.Equal(t, 50.0, sums2[0].FundingNetProfit)

	// Query for all (empty accountID)
	sumsAll, err := repo.GetAccountSymbolPnLSummaries(ctx, "", "mexc", since)
	require.NoError(t, err)
	require.Len(t, sumsAll, 1)
	assert.Equal(t, 150.0, sumsAll[0].FundingNetProfit)
}
