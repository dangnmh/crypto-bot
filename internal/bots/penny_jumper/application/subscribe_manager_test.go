package application_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/penny_jumper/application"
	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store/orderbook"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubscribeManager_DynamicUniverse(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	cfg := pjdomain.PennyJumperConfig{
		Exchanges: []string{"toobit"},
		Universe: pjdomain.UniverseConfig{
			TopGainerLimit:   30,
			MinVolume24hUSDT: 100000,
			MaxCoinPrice:     10.0,
		},
	}
	sub := newMockDepthSubscriber()

	mockFetcher := &mockTopGainerFetcher{
		gainers: []exchange.TopGainerResult{
			{Symbol: "DOGEUSDT", LastPrice: 0.15, Gain24hPct: 25.0, Volume24hUSDT: 500000},
			{Symbol: "PEPEUSDT", LastPrice: 0.00001, Gain24hPct: 20.0, Volume24hUSDT: 400000},
			{Symbol: "LOWVOL", LastPrice: 1.0, Gain24hPct: 30.0, Volume24hUSDT: 5000},
			{Symbol: "BLOCKED", LastPrice: 2.0, Gain24hPct: 40.0, Volume24hUSDT: 600000},
		},
	}

	mockSnap := &mockDepthProvider{}
	syncMgr := orderbook.NewSynchronizer(orderbook.SynchronizerConfig{
		Exchange:           "toobit",
		Mode:               orderbook.SyncModeSnapshot,
		MaxBufferCapacity:  500,
		SnapshotTimeout:    5 * time.Second,
		CommitRecoverySize: 1000,
	}, mockSnap, nil, logger)

	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	clients := []application.ExchangeClient{
		{
			Exchange:     "toobit",
			Fetcher:      mockFetcher,
			Subscriber:   sub,
			Synchronizer: syncMgr,
			DepthStore:   depthStore,
		},
	}

	sm, err := application.NewSubscribeManager(
		cfg,
		clients,
		[]string{"BLOCKED"},
		logger,
	)
	require.NoError(t, err)

	ctx := context.Background()

	// 1. Initial refresh
	universe, err := sm.RefreshUniverse(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"DOGEUSDT", "PEPEUSDT"}, universe)
	assert.True(t, sub.IsSubscribed("DOGEUSDT"))
	assert.True(t, sub.IsSubscribed("PEPEUSDT"))

	// 2. Refresh universe where PEPEUSDT dropped out, replaced by SOLUSDT
	mockFetcher.gainers = []exchange.TopGainerResult{
		{Symbol: "DOGEUSDT", LastPrice: 0.15, Gain24hPct: 25.0, Volume24hUSDT: 500000},
		{Symbol: "SHIBUSDT", LastPrice: 0.00002, Gain24hPct: 15.0, Volume24hUSDT: 300000},
	}

	universe2, err := sm.RefreshUniverse(ctx)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"DOGEUSDT", "SHIBUSDT"}, universe2)
	assert.True(t, sub.IsSubscribed("SHIBUSDT"))
	assert.True(t, sub.IsUnsubscribed("PEPEUSDT"))
}

func TestSubscribeManager_MultiExchangeUniverse(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	cfg := pjdomain.PennyJumperConfig{
		Exchanges: []string{"toobit", "mexc"},
		Universe: pjdomain.UniverseConfig{
			TopGainerLimit:   30,
			MinVolume24hUSDT: 100000,
			MaxCoinPrice:     10.0,
		},
	}

	subToobit := newMockDepthSubscriber()
	subMEXC := newMockDepthSubscriber()

	fetcherToobit := &mockTopGainerFetcher{
		gainers: []exchange.TopGainerResult{
			{Symbol: "ADAUSDT", LastPrice: 0.50, Gain24hPct: 25.0, Volume24hUSDT: 500000},
		},
	}
	fetcherMEXC := &mockTopGainerFetcher{
		gainers: []exchange.TopGainerResult{
			{Symbol: "DOT_USDT", LastPrice: 4.50, Gain24hPct: 30.0, Volume24hUSDT: 600000},
		},
	}

	mockSnap := &mockDepthProvider{}
	syncToobit := orderbook.NewSynchronizer(orderbook.SynchronizerConfig{
		Exchange:           "toobit",
		Mode:               orderbook.SyncModeSnapshot,
		MaxBufferCapacity:  500,
		SnapshotTimeout:    5 * time.Second,
		CommitRecoverySize: 1000,
	}, mockSnap, nil, logger)
	syncMEXC := orderbook.NewSynchronizer(orderbook.SynchronizerConfig{
		Exchange:           "mexc",
		Mode:               orderbook.SyncModeIncremental,
		MaxBufferCapacity:  500,
		SnapshotTimeout:    5 * time.Second,
		CommitRecoverySize: 1000,
	}, mockSnap, nil, logger)

	depthToobit := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	depthMEXC := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	clients := []application.ExchangeClient{
		{
			Exchange:     "toobit",
			Fetcher:      fetcherToobit,
			Subscriber:   subToobit,
			Synchronizer: syncToobit,
			DepthStore:   depthToobit,
		},
		{
			Exchange:     "mexc",
			Fetcher:      fetcherMEXC,
			Subscriber:   subMEXC,
			Synchronizer: syncMEXC,
			DepthStore:   depthMEXC,
		},
	}

	sm, err := application.NewSubscribeManager(
		cfg,
		clients,
		nil,
		logger,
	)
	require.NoError(t, err)

	ctx := t.Context()
	universe, err := sm.RefreshUniverse(ctx)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"ADAUSDT", "DOT_USDT"}, universe)
	assert.True(t, subToobit.IsSubscribed("ADAUSDT"))
	assert.True(t, subMEXC.IsSubscribed("DOT_USDT"))

	// Verify UnsubscribeAll on shutdown
	sm.UnsubscribeAll(ctx)
	assert.True(t, subToobit.IsUnsubscribed("ADAUSDT"))
	assert.True(t, subMEXC.IsUnsubscribed("DOT_USDT"))
	assert.Empty(t, sm.CurrentUniverse())
}

func TestSubscribeManager_MaxCoinPriceFilter(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	cfg := pjdomain.PennyJumperConfig{
		Exchanges: []string{"toobit"},
		Universe: pjdomain.UniverseConfig{
			TopGainerLimit:   30,
			MinVolume24hUSDT: 100000,
			MaxCoinPrice:     5.0, // Only coins <= $5.00
		},
	}
	sub := newMockDepthSubscriber()

	mockFetcher := &mockTopGainerFetcher{
		gainers: []exchange.TopGainerResult{
			{Symbol: "BTCUSDT", LastPrice: 95000.0, Gain24hPct: 35.0, Volume24hUSDT: 5000000}, // Excluded (price > 5.0)
			{Symbol: "ETHUSDT", LastPrice: 3500.0, Gain24hPct: 30.0, Volume24hUSDT: 4000000},  // Excluded (price > 5.0)
			{Symbol: "SOLUSDT", LastPrice: 180.0, Gain24hPct: 28.0, Volume24hUSDT: 3000000},   // Excluded (price > 5.0)
			{Symbol: "DOGEUSDT", LastPrice: 0.15, Gain24hPct: 25.0, Volume24hUSDT: 2000000},   // Qualified
			{Symbol: "XRPUSDT", LastPrice: 2.20, Gain24hPct: 22.0, Volume24hUSDT: 1500000},    // Qualified
		},
	}

	mockSnap := &mockDepthProvider{}
	syncMgr := orderbook.NewSynchronizer(orderbook.SynchronizerConfig{
		Exchange:           "toobit",
		Mode:               orderbook.SyncModeSnapshot,
		MaxBufferCapacity:  500,
		SnapshotTimeout:    5 * time.Second,
		CommitRecoverySize: 1000,
	}, mockSnap, nil, logger)

	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	clients := []application.ExchangeClient{
		{
			Exchange:     "toobit",
			Fetcher:      mockFetcher,
			Subscriber:   sub,
			Synchronizer: syncMgr,
			DepthStore:   depthStore,
		},
	}

	sm, err := application.NewSubscribeManager(
		cfg,
		clients,
		nil,
		logger,
	)
	require.NoError(t, err)

	ctx := context.Background()
	universe, err := sm.RefreshUniverse(ctx)
	require.NoError(t, err)

	assert.ElementsMatch(t, []string{"DOGEUSDT", "XRPUSDT"}, universe)
	assert.False(t, sub.IsSubscribed("BTCUSDT"))
	assert.False(t, sub.IsSubscribed("ETHUSDT"))
	assert.False(t, sub.IsSubscribed("SOLUSDT"))
	assert.True(t, sub.IsSubscribed("DOGEUSDT"))
	assert.True(t, sub.IsSubscribed("XRPUSDT"))
}
