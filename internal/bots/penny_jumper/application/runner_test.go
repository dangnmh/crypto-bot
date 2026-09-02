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
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/testutil/mocks"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"
	"crypto-bot/pkg/xjson"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPennyJumperRunner_InitValidation(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	cfg := pjdomain.PennyJumperConfig{Exchanges: []string{"toobit"}}

	_, err := application.NewPennyJumperRunner(cfg, nil, nil, nil, nil, nil, nil, nil, nil)
	require.Error(t, err)

	repo := persistence.NewGormWallRepository(nil)
	_, err = application.NewPennyJumperRunner(cfg, nil, nil, nil, repo, nil, nil, bus, logger)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "contractStores is required")
}

func TestPennyJumperRunner_FullEventSourcedPipeline(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "BTCUSDT", PriceUnit: 0.01, ContractSize: 1.0},
	}, nil).AnyTimes()

	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	cStore := store.NewContractStore(&sync.WaitGroup{}, slog.Default())
	ctx := t.Context()
	go cStore.StartContractSync(ctx, mockClient, time.Hour)

	// Wait for contract store to sync
	require.Eventually(t, func() bool {
		_, err := cStore.GetContract(ctx, "BTCUSDT")
		return err == nil
	}, 2*time.Second, 10*time.Millisecond)

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&persistence.PennyJumperWallRecord{}))
	repo := persistence.NewGormWallRepository(db)

	cfg := pjdomain.PennyJumperConfig{
		Exchanges: []string{"toobit"},
		WallDetector: pjdomain.WallDetectorConfig{
			MinVolumeUSDT:      20000.0,
			MinLifespan:        types.Duration(2 * time.Second),
			MaxWallDistancePct: 1.0,
			MaxSpreadPct:       0.3,
		},
	}
	cfg.ApplyDefaults()

	wallDetector := application.NewWallDetector("toobit", cfg.WallDetector, depthStore, cStore, bus, logger)
	wallJudge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.60})

	runner, err := application.NewPennyJumperRunner(
		cfg,
		map[string]*pjstore.DepthStore{"toobit": depthStore},
		map[string]*application.WallDetector{"toobit": wallDetector},
		wallJudge,
		repo,
		map[string]*store.ContractStore{"toobit": cStore},
		mockNotifier,
		bus,
		logger,
	)
	require.NoError(t, err)

	// Register all global subscriptions
	application.InitGlobalSubscriptions(ctx, runner)

	// Subscribe to TopicWallQualified to verify local model qualification
	qualifiedCh, err := bus.Subscribe(ctx, pjdomain.TopicWallQualified)
	require.NoError(t, err)

	// 1. Emit DepthUpdatedEvent with a large wall (WALL_BORN)
	now := time.Now()
	ob1 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1001,
		Bids: []shared.OrderBookEntry{
			{Price: 60000.0, Volume: 50.0}, // Massive wall (50x)
			{Price: 59990.0, Volume: 1.0},
			{Price: 59980.0, Volume: 1.0},
			{Price: 59970.0, Volume: 1.0},
			{Price: 59960.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60010.0, Volume: 0.1},
		},
	}

	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "BTCUSDT",
		Version:   1001,
		OrderBook: ob1,
		Timestamp: now,
	})
	require.NoError(t, err)

	// In-memory active wall exists, but DB is NOT written while wall is alive
	require.Eventually(t, func() bool {
		_, found := depthStore.GetActiveWall("BTCUSDT", shared.SideOpenLong)
		return found
	}, 2*time.Second, 10*time.Millisecond)

	recordsBeforeDisappear, err := repo.List(ctx, "BTCUSDT", 10)
	require.NoError(t, err)
	assert.Empty(t, recordsBeforeDisappear, "No DB record should be saved while wall is still alive")

	// 2. Emit orderbook where volume decreases from 50.0 to 40.0 (Taker Absorption)
	depthStore.RecordPublicTrades("BTCUSDT", []shared.PublicTrade{
		{
			Symbol:    "BTCUSDT",
			Price:     60000.0,
			Volume:    10.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(5 * time.Second),
		},
	})
	ob2 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1002,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60000.0, Volume: 40.0},
			{Price: 59990.0, Volume: 1.0},
			{Price: 59980.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60010.0, Volume: 0.1},
		},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "BTCUSDT",
		Version:   1002,
		OrderBook: ob2,
		Timestamp: now.Add(5 * time.Second),
	})
	require.NoError(t, err)

	// Verify Wall is Qualified by WallJudge on TopicWallQualified
	select {
	case msg := <-qualifiedCh:
		var qEvt pjdomain.WallQualifiedEvent
		err := xjson.Unmarshal(msg.Payload, &qEvt)
		require.NoError(t, err)
		assert.Equal(t, "BTCUSDT", qEvt.Wall.Symbol)
		assert.GreaterOrEqual(t, qEvt.TrustScore, 0.60)
		assert.InDelta(t, 60000.01, qEvt.TargetEntryPrice, 1e-2)
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for TopicWallQualified")
	}

	// 3. Emit orderbook where wall disappeared (Pulled) - Volume 0.1 < $20k notional
	// Wall is immediately finalized as WALL_DISAPPEARED and persisted to DB
	ob3 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1003,
		Bids: []shared.OrderBookEntry{
			{Price: 59990.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60010.0, Volume: 0.1},
		},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "BTCUSDT",
		Version:   1003,
		OrderBook: ob3,
		Timestamp: now.Add(6 * time.Second),
	})
	require.NoError(t, err)

	// Verify complete event stream and record is persisted to DB upon disappearance
	require.Eventually(t, func() bool {
		records, err := repo.List(ctx, "BTCUSDT", 10)
		return err == nil && len(records) == 1 && records[0].Outcome == "WALL_DISAPPEARED"
	}, 2*time.Second, 10*time.Millisecond)

	records, err := repo.List(ctx, "BTCUSDT", 10)
	require.NoError(t, err)
	require.Len(t, records, 1)
	assert.Equal(t, "WALL_DISAPPEARED", records[0].Outcome)
	assert.Equal(t, 50.0, records[0].InitialVolume)
	assert.Equal(t, 40.0, records[0].FinalVolume)
	assert.Equal(t, 10.0, records[0].AbsorbedVolume)

	events, err := records[0].GetEvents()
	require.NoError(t, err)
	require.Len(t, events, 4)
	assert.Equal(t, pjdomain.WallEventBorn, events[0].EventType)
	assert.Equal(t, pjdomain.WallEventMatured, events[1].EventType)
	assert.Equal(t, pjdomain.WallEventAbsorbed, events[2].EventType)
	assert.Equal(t, pjdomain.WallEventDisappeared, events[3].EventType)
}

func TestFormatWallDetectedNotification(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 23, 17, 0, 0, 0, time.UTC)
	firstDetected := now.Add(-2 * time.Second)
	wall := pjdomain.Wall{
		ID:              "test-wall-123",
		Exchange:        "mexc_spot",
		Symbol:          "ZROUSDT",
		Side:            shared.SideOpenLong,
		Price:           1.2138,
		Volume:          17309.3582,
		DistancePct:     0.26,
		FirstDetectedAt: firstDetected,
	}

	// Without 24h volume
	msg := application.FormatWallDetectedNotification(&wall, 0.10, 21010.10, 0, 0, now)
	expected := "🟢 [PENNY_JUMPER] [mexc_spot] [WALL_QUALIFIED]\n" +
		"• Symbol: ZROUSDT | Side: Long\n" +
		"• Price: 1.213800 | Size: 21,010.10 USDT\n" +
		"• Dist: 0.26% | Spread: 0.10%\n" +
		"• Wall Age: 2s"
	assert.Equal(t, expected, msg)

	// With 24h volume and 1m turnover ratio
	msgWithRatio := application.FormatWallDetectedNotification(&wall, 0.10, 21010.10, 6000000.0, 5.0, now)
	expectedWithRatio := "🟢 [PENNY_JUMPER] [mexc_spot] [WALL_QUALIFIED]\n" +
		"• Symbol: ZROUSDT | Side: Long\n" +
		"• Price: 1.213800 | Size: 21,010.10 USDT (5.0x 1m Vol | 24h Vol: 6,000,000.00 USDT)\n" +
		"• Dist: 0.26% | Spread: 0.10%\n" +
		"• Wall Age: 2s"
	assert.Equal(t, expectedWithRatio, msgWithRatio)

	// With Depth Imbalance & Backing Ratio
	wallWithImbalance := wall
	wallWithImbalance.DepthImbalance = 3.2
	wallWithImbalance.BackingRatio = 1.5
	msgWithImbalance := application.FormatWallDetectedNotification(&wallWithImbalance, 0.10, 21010.10, 6000000.0, 5.0, now)
	expectedWithImbalance := "🟢 [PENNY_JUMPER] [mexc_spot] [WALL_QUALIFIED]\n" +
		"• Symbol: ZROUSDT | Side: Long\n" +
		"• Price: 1.213800 | Size: 21,010.10 USDT (5.0x 1m Vol | 24h Vol: 6,000,000.00 USDT)\n" +
		"• Dist: 0.26% | Spread: 0.10%\n" +
		"• Imbalance: 3.2x | Backing: 1.5x\n" +
		"• Wall Age: 2s"
	assert.Equal(t, expectedWithImbalance, msgWithImbalance)

	// With 1h History
	wallWithHistory := wallWithImbalance
	wallWithHistory.PullCount1h = 1
	wallWithHistory.FillCount1h = 2
	msgWithHistory := application.FormatWallDetectedNotification(&wallWithHistory, 0.10, 21010.10, 6000000.0, 5.0, now)
	expectedWithHistory := "🟢 [PENNY_JUMPER] [mexc_spot] [WALL_QUALIFIED]\n" +
		"• Symbol: ZROUSDT | Side: Long\n" +
		"• Price: 1.213800 | Size: 21,010.10 USDT (5.0x 1m Vol | 24h Vol: 6,000,000.00 USDT)\n" +
		"• Dist: 0.26% | Spread: 0.10%\n" +
		"• Imbalance: 3.2x | Backing: 1.5x\n" +
		"• 1h History: 1 Pulls / 2 Fills\n" +
		"• Wall Age: 2s"
	assert.Equal(t, expectedWithHistory, msgWithHistory)
}

func TestPennyJumperRunner_WallEligibilityDualVolumeCheck(t *testing.T) {
	t.Parallel()

	ctrl := gomock.NewController(t)
	mockClient := mocks.NewMockClient(ctrl)
	mockClient.EXPECT().GetContractDetails(gomock.Any()).Return([]exchange.ContractDetail{
		{Symbol: "ETHUSDT", PriceUnit: 0.01, ContractSize: 1.0},
		{Symbol: "SOLUSDT", PriceUnit: 0.01, ContractSize: 1.0},
	}, nil).AnyTimes()

	cStore := store.NewContractStore(&sync.WaitGroup{}, slog.Default())
	ctx := t.Context()
	go cStore.StartContractSync(ctx, mockClient, time.Hour)
	require.Eventually(t, func() bool {
		_, err1 := cStore.GetContract(ctx, "ETHUSDT")
		_, err2 := cStore.GetContract(ctx, "SOLUSDT")
		return err1 == nil && err2 == nil
	}, 2*time.Second, 10*time.Millisecond)

	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	// 24h Volume = 14,400,000 USDT -> 1m Volume = 10,000 USDT
	depthStore.SaveVolume24h("ETHUSDT", 14400000.0)
	depthStore.SaveVolume24h("SOLUSDT", 14400000.0)

	cfg := pjdomain.PennyJumperConfig{
		Exchanges: []string{"toobit"},
		WallDetector: pjdomain.WallDetectorConfig{
			MinVolumeUSDT:       20000.0, // Static: >= 20k USDT
			MinWallTo1mVolRatio: 3.0,     // Dynamic: >= 3x of 1m turnover (3 * 10k = 30k USDT required)
			MinLifespan:         types.Duration(2 * time.Second),
		},
	}
	cfg.ApplyDefaults()

	logger := slog.Default()
	bus := eventbus.New(logger)
	wallDetector := application.NewWallDetector("toobit", cfg.WallDetector, depthStore, cStore, bus, logger)
	wallJudge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.60})
	repo := persistence.NewGormWallRepository(nil)

	mockNotifier := mocks.NewMockNotifier(ctrl)
	mockNotifier.EXPECT().Send(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	runner, err := application.NewPennyJumperRunner(
		cfg,
		map[string]*pjstore.DepthStore{"toobit": depthStore},
		map[string]*application.WallDetector{"toobit": wallDetector},
		wallJudge,
		repo,
		map[string]*store.ContractStore{"toobit": cStore},
		mockNotifier,
		bus,
		logger,
	)
	require.NoError(t, err)

	application.InitGlobalSubscriptions(ctx, runner)
	qualifiedCh, err := bus.Subscribe(ctx, pjdomain.TopicWallQualified)
	require.NoError(t, err)

	now := time.Now()

	// 1. SOLUSDT: Wall with 25k USDT -> Passes static (>= 20k) but FAILS dynamic (2.5x < 3.0x)
	obSol1 := &shared.OrderBook{
		Symbol:  "SOLUSDT",
		Version: 101,
		Bids: []shared.OrderBookEntry{
			{Price: 200.0, Volume: 150.0}, // 30,000 USDT initial
			{Price: 199.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{{Price: 201.0, Volume: 1.0}},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "SOLUSDT",
		Version:   101,
		OrderBook: obSol1,
		Timestamp: now,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, found := depthStore.GetActiveWall("SOLUSDT", shared.SideOpenLong)
		return found
	}, 2*time.Second, 10*time.Millisecond)

	depthStore.RecordPublicTrades("SOLUSDT", []shared.PublicTrade{
		{
			Symbol:    "SOLUSDT",
			Price:     200.0,
			Volume:    25.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(3 * time.Second),
		},
	})
	obSol2 := &shared.OrderBook{
		Symbol:  "SOLUSDT",
		Version: 102,
		Bids: []shared.OrderBookEntry{
			{Price: 200.0, Volume: 125.0}, // 25,000 USDT (2.5x 1m turnover < 3.0x required)
			{Price: 199.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{{Price: 201.0, Volume: 1.0}},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "SOLUSDT",
		Version:   102,
		OrderBook: obSol2,
		Timestamp: now.Add(3 * time.Second),
	})
	require.NoError(t, err)

	select {
	case <-qualifiedCh:
		t.Fatal("SOLUSDT wall with 2.5x 1m turnover should NOT qualify when 3.0x is required")
	case <-time.After(50 * time.Millisecond):
		// Expected: filtered out by dynamic volume ratio
	}

	// 2. ETHUSDT: Wall with 40k USDT -> Passes BOTH static (>= 20k) and dynamic (4.0x >= 3.0x)
	obEth1 := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 201,
		Bids: []shared.OrderBookEntry{
			{Price: 2500.0, Volume: 20.0}, // 50,000 USDT initial
			{Price: 2490.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{{Price: 2510.0, Volume: 0.1}},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "ETHUSDT",
		Version:   201,
		OrderBook: obEth1,
		Timestamp: now,
	})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		_, found := depthStore.GetActiveWall("ETHUSDT", shared.SideOpenLong)
		return found
	}, 2*time.Second, 10*time.Millisecond)

	depthStore.RecordPublicTrades("ETHUSDT", []shared.PublicTrade{
		{
			Symbol:    "ETHUSDT",
			Price:     2500.0,
			Volume:    4.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(3 * time.Second),
		},
	})
	obEth2 := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 202,
		Bids: []shared.OrderBookEntry{
			{Price: 2500.0, Volume: 16.0}, // 40,000 USDT (4.0x 1m turnover >= 3.0x required)
			{Price: 2490.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{{Price: 2510.0, Volume: 0.1}},
	}
	err = bus.Publish(pjdomain.TopicDepthUpdated, pjdomain.DepthUpdatedEvent{
		Exchange:  "toobit",
		Symbol:    "ETHUSDT",
		Version:   202,
		OrderBook: obEth2,
		Timestamp: now.Add(3 * time.Second),
	})
	require.NoError(t, err)

	select {
	case msg := <-qualifiedCh:
		var qEvt pjdomain.WallQualifiedEvent
		err := xjson.Unmarshal(msg.Payload, &qEvt)
		require.NoError(t, err)
		assert.Equal(t, "ETHUSDT", qEvt.Wall.Symbol)
		msg.Ack()
	case <-time.After(2 * time.Second):
		t.Fatal("ETHUSDT wall with 4.0x 1m turnover should qualify")
	}
}
