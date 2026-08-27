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

	db, err := gorm.Open(sqlite.Open("file:runner_test?mode=memory&cache=shared&_busy_timeout=5000"), &gorm.Config{})
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
	assert.Equal(t, int64(6000), records[0].DurationMs)

	events, err := records[0].GetEvents()
	require.NoError(t, err)
	require.Len(t, events, 4)
	assert.Equal(t, pjdomain.WallEventBorn, events[0].EventType)
	assert.Equal(t, pjdomain.WallEventMatured, events[1].EventType)
	assert.Equal(t, pjdomain.WallEventResized, events[2].EventType)
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

	msg := application.FormatWallDetectedNotification(&wall, 0.10, 21010.10, now)
	expected := "🟢 [PENNY_JUMPER] [mexc_spot] [WALL_QUALIFIED]\n" +
		"• Symbol: ZROUSDT | Side: Long\n" +
		"• Price: 1.213800 | Size: 21,010.10 USDT\n" +
		"• Dist: 0.26% | Spread: 0.10%\n" +
		"• Wall Age: 2s"

	assert.Equal(t, expected, msg)
}
