package application_test

import (
	"log/slog"
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/bots/penny_jumper/application"
	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/persistence"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	infraapp "crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/store/orderbook"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPennyJumperBot_LifecycleAndDepthProcessing(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.PennyJumperConfig{
		Exchanges:     []string{"toobit"},
		ExecutionMode: pjdomain.ExecutionModePaper,
		Universe: pjdomain.UniverseConfig{
			MinVolume24hUSDT: 100000,
			MaxCoinPrice:     10.0,
			TickerInterval:   types.Duration(100 * time.Millisecond),
		},
		WallDetector: pjdomain.WallDetectorConfig{
			MinVolumeUSDT:      20000,
			MaxWallDistancePct: 1.0,
			MaxSpreadPct:       0.3,
		},
	}

	var wg sync.WaitGroup
	cStore := store.NewContractStore(&wg, logger)
	wg.Done()
	wallDetector := application.NewWallDetector("toobit", cfg.WallDetector, depthStore, cStore, bus, logger)
	subMock := newMockDepthSubscriber()
	mockFetcher := &mockTopGainerFetcher{
		gainers: []exchange.TopGainerResult{
			{Symbol: "DOGEUSDT", LastPrice: 0.15, Gain24hPct: 10.0, Volume24hUSDT: 500000},
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
	repo := persistence.NewGormWallRepository(nil)
	clients := []application.ExchangeClient{
		{
			Exchange:     "toobit",
			Fetcher:      mockFetcher,
			Subscriber:   subMock,
			Synchronizer: syncMgr,
			DepthStore:   depthStore,
		},
	}
	subMgr, err := application.NewSubscribeManager(cfg, clients, nil, logger)
	require.NoError(t, err)

	notif, err := notifier.NewFromConfig(notifier.Config{}, logger)
	require.NoError(t, err)

	wallJudge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{})

	runner, err := application.NewPennyJumperRunner(
		cfg,
		map[string]*pjstore.DepthStore{"toobit": depthStore},
		map[string]*application.WallDetector{"toobit": wallDetector},
		wallJudge,
		repo,
		map[string]*store.ContractStore{"toobit": cStore},
		notif,
		bus,
		logger,
	)
	require.NoError(t, err)

	bot := application.NewPennyJumperBot(
		cfg,
		&infraapp.Engine{},
		notif,
		subMgr,
		runner,
		map[string]*pjstore.DepthStore{"toobit": depthStore},
		map[string]*store.ContractStore{"toobit": cStore},
		map[string]orderbook.Synchronizer{"toobit": syncMgr},
		bus,
		logger,
	)

	assert.Equal(t, "penny_jumper", bot.Name())

	ctx := t.Context()

	err = bot.Start(ctx)
	require.NoError(t, err)

	// Publish depth event with qualified wall
	now := time.Now()
	ob := &shared.OrderBook{
		Symbol:  "DOGEUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 0.1500, Volume: 1000.0},
			{Price: 0.1499, Volume: 1000.0},
			{Price: 0.1498, Volume: 1000.0},
			{Price: 0.1497, Volume: 1000.0},
			{Price: 0.1496, Volume: 1000.0},
			{Price: 0.1490, Volume: 500000.0}, // 500x avg
		},
		Asks: []shared.OrderBookEntry{
			{Price: 0.1501, Volume: 1000.0},
		},
	}

	_ = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "DOGEUSDT",
		Version:   1,
		OrderBook: ob,
		Timestamp: now,
	})

	require.Eventually(t, func() bool {
		_, ok := depthStore.GetLatestDepth("DOGEUSDT")
		return ok
	}, time.Second, 10*time.Millisecond)

	// Stop bot
	err = bot.Stop(ctx)
	require.NoError(t, err)
}
