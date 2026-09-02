package application_test

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/penny_jumper/application"
	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	pjstore "crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/pkg/eventbus"
	"crypto-bot/pkg/types"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWallDetector_DetectsAndEmitsEvents(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.3,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. First orderbook snapshot with huge bid wall at index 5 (60000.0 * 50 = $3,000,000 > $20k)
	ob1 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
			{Price: 60003.0, Volume: 0.1},
			{Price: 60002.0, Volume: 0.1},
			{Price: 60001.0, Volume: 0.1},
			{Price: 60000.0, Volume: 50.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}

	walls1 := detector.ProcessOrderBook(context.Background(), ob1, now)
	require.Len(t, walls1, 1)
	assert.Equal(t, "toobit", walls1[0].Exchange)
	assert.Equal(t, 60000.0, walls1[0].Price)
	assert.Equal(t, 50.0, walls1[0].Volume)

	activeWall, found := depthStore.GetActiveWall("BTCUSDT", shared.SideOpenLong)
	require.True(t, found)
	assert.Equal(t, 60000.0, activeWall.Price)

	// 2. Second orderbook snapshot where wall changes volume
	ob2 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
			{Price: 60003.0, Volume: 0.1},
			{Price: 60002.0, Volume: 0.1},
			{Price: 60001.0, Volume: 0.1},
			{Price: 60000.0, Volume: 60.0}, // resized
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}

	walls2 := detector.ProcessOrderBook(context.Background(), ob2, now.Add(time.Second))
	require.Len(t, walls2, 1)
	assert.Equal(t, 60.0, walls2[0].Volume)

	// 3. Third orderbook snapshot where wall disappeared (pulled)
	ob3 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 3,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}

	walls3 := detector.ProcessOrderBook(context.Background(), ob3, now.Add(2*time.Second))
	assert.Empty(t, walls3)

	_, foundAfterPull := depthStore.GetActiveWall("BTCUSDT", shared.SideOpenLong)
	assert.False(t, foundAfterPull)
}

func TestWallDetector_MinLifespanMaturation(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0,
		MinLifespan:        types.Duration(5 * time.Second),
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.5,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("mexc_futures", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	ob := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
			{Price: 60000.0, Volume: 50.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}

	// 1. T=0s: Wall detected immediately and registered
	walls0 := detector.ProcessOrderBook(context.Background(), ob, now)
	require.Len(t, walls0, 1)
	assert.Equal(t, 60000.0, walls0[0].Price)

	w0, found := depthStore.GetActiveWall("BTC_USDT", shared.SideOpenLong)
	require.True(t, found)
	assert.Equal(t, 60000.0, w0.Price)

	// 2. T=2s: Still active (< 5s)
	walls2 := detector.ProcessOrderBook(context.Background(), ob, now.Add(2*time.Second))
	require.Len(t, walls2, 1)

	// 3. T=5.1s: Maturation reached (>= 5s), matures and continues tracking
	walls5 := detector.ProcessOrderBook(context.Background(), ob, now.Add(5100*time.Millisecond))
	require.Len(t, walls5, 1)
	assert.Equal(t, 60000.0, walls5[0].Price)

	w5, _ := depthStore.GetActiveWall("BTC_USDT", shared.SideOpenLong)
	assert.Equal(t, 60000.0, w5.Price)

	// 4. T=6s: Normal volume update while active
	obResized := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 4,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
			{Price: 60000.0, Volume: 60.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}
	walls6 := detector.ProcessOrderBook(context.Background(), obResized, now.Add(6*time.Second))
	require.Len(t, walls6, 1)
	assert.Equal(t, 60.0, walls6[0].Volume)
}

func TestWallDetector_MinLifespanSpoofingCancelled(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0,
		MinLifespan:        types.Duration(5 * time.Second),
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.5,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("mexc_futures", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// Spoof wall appears at T=0
	ob1 := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60000.0, Volume: 100.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}
	walls1 := detector.ProcessOrderBook(context.Background(), ob1, now)
	require.Len(t, walls1, 1)

	// Spoof wall pulled at T=1s (before 5s)
	ob2 := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 60005.0, Volume: 0.1},
			{Price: 60004.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60006.0, Volume: 0.1},
		},
	}
	walls2 := detector.ProcessOrderBook(context.Background(), ob2, now.Add(1*time.Second))
	assert.Empty(t, walls2)

	_, found := depthStore.GetActiveWall("BTC_USDT", shared.SideOpenLong)
	assert.False(t, found, "Spoof wall should be purged from DepthStore")
}

type mockContractReader struct {
	contracts map[string]*store.ContractData
}

func (m *mockContractReader) GetContract(_ context.Context, symbol string) (*store.ContractData, error) {
	if cd, ok := m.contracts[symbol]; ok {
		return cd, nil
	}
	return nil, nil
}

func TestWallDetector_MinVolumeUSDTFiltering(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	contractReader := &mockContractReader{
		contracts: map[string]*store.ContractData{
			"BTC_USDT": {
				Symbol:       "BTC_USDT",
				PriceUnit:    0.1,
				ContractSize: 0.001, // 1 contract = 0.001 BTC
			},
		},
	}

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0, // Minimum $20k USDT
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	detector := application.NewWallDetector("mexc_futures", cfg, depthStore, contractReader, bus, logger)

	now := time.Now()

	// Case 1: 60,000 price * 100 contracts * 0.001 = $6,000 (< $20k) -> Rejected
	obSmall := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 60000.0, Volume: 100.0},
			{Price: 59999.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60001.0, Volume: 1.0},
		},
	}
	wallsSmall := detector.ProcessOrderBook(context.Background(), obSmall, now)
	assert.Empty(t, wallsSmall)

	// Case 2: 60,000 price * 500 contracts * 0.001 = $30,000 (>= $20k) -> Accepted
	obLarge := &shared.OrderBook{
		Symbol:  "BTC_USDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 60000.0, Volume: 500.0},
			{Price: 59999.0, Volume: 1.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60001.0, Volume: 1.0},
		},
	}
	wallsLarge := detector.ProcessOrderBook(context.Background(), obLarge, now)
	require.Len(t, wallsLarge, 1)
	assert.Equal(t, 60000.0, wallsLarge[0].Price)
	assert.Equal(t, 500.0, wallsLarge[0].Volume)
}

func TestWallDetector_SpreadFilter(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      1000.0,
		MaxSpreadPct:       0.2, // 0.2% max spread
		MaxWallDistancePct: 1.0,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// Spread = (60300 - 60000) / 60000 = 0.5% > 0.2% -> Rejected
	obWide := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 60000.0, Volume: 10.0},
			{Price: 59990.0, Volume: 10.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 60300.0, Volume: 10.0},
			{Price: 60310.0, Volume: 10.0},
		},
	}

	wallsWide := detector.ProcessOrderBook(context.Background(), obWide, now)
	assert.Empty(t, wallsWide)
}

func TestWallDetector_InmemoryEventSourcingStream(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      1000.0,
		MinLifespan:        types.Duration(2 * time.Second),
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.5,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. Birth
	ob1 := &shared.OrderBook{
		Symbol:  "SOLUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 150.0, Volume: 0.1},
			{Price: 149.0, Volume: 500.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 150.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob1, now)

	activeWall, found := depthStore.GetActiveWall("SOLUSDT", shared.SideOpenLong)
	require.True(t, found)
	wallID := activeWall.ID

	events := depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 1)
	assert.Equal(t, int64(1), events[0].Seq)
	assert.Equal(t, pjdomain.WallEventBorn, events[0].EventType)
	assert.InDelta(t, 0.0667, events[0].SpreadPct, 1e-3)

	// 2. Maturation at 2s
	detector.ProcessOrderBook(context.Background(), ob1, now.Add(2*time.Second))
	events = depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 2)
	assert.Equal(t, int64(2), events[1].Seq)
	assert.Equal(t, pjdomain.WallEventMatured, events[1].EventType)

	// 3. Taker Absorption (500 -> 450)
	trades := []shared.PublicTrade{
		{
			Symbol:    "SOLUSDT",
			Price:     149.0,
			Volume:    50.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(3 * time.Second),
		},
	}
	depthStore.RecordPublicTrades("SOLUSDT", trades)
	ob2 := &shared.OrderBook{
		Symbol:  "SOLUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 150.0, Volume: 0.1},
			{Price: 149.0, Volume: 450.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 150.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob2, now.Add(3*time.Second))
	events = depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 3)
	assert.Equal(t, int64(3), events[2].Seq)
	assert.Equal(t, pjdomain.WallEventAbsorbed, events[2].EventType)
	assert.InDelta(t, -50.0, events[2].DeltaVolume, 1e-6)

	metrics := pjdomain.ReconcileWallData(activeWall, events, trades)
	assert.InDelta(t, 50.0, metrics.AbsorbedVolume, 1e-6)
	assert.InDelta(t, 0.0, metrics.PulledVolume, 1e-6)

	// 4. Disappearance (Pulled)
	ob3 := &shared.OrderBook{
		Symbol:  "SOLUSDT",
		Version: 3,
		Bids: []shared.OrderBookEntry{
			{Price: 150.0, Volume: 0.1},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 150.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob3, now.Add(4*time.Second))
	events = depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 4)
	assert.Equal(t, int64(4), events[3].Seq)
	assert.Equal(t, pjdomain.WallEventDisappeared, events[3].EventType)
}

func TestWallDetector_FlappingDetection(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      1000.0,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.5,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. Initial Wall: 1000 volume (WALL_BORN)
	ob1 := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 3000.0, Volume: 0.1},
			{Price: 2990.0, Volume: 1000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 3001.0, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob1, now)

	w, found := depthStore.GetActiveWall("ETHUSDT", shared.SideOpenLong)
	require.True(t, found)
	wallID := w.ID

	// 2. Drop 1: 1000 -> 800 (WALL_ABSORBED due to taker trades, delta=-200)
	trades := []shared.PublicTrade{
		{
			Symbol:    "ETHUSDT",
			Price:     2990.0,
			Volume:    200.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(1 * time.Second),
		},
	}
	depthStore.RecordPublicTrades("ETHUSDT", trades)
	ob2 := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 3000.0, Volume: 0.1},
			{Price: 2990.0, Volume: 800.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 3001.0, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob2, now.Add(1*time.Second))

	events := depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 2)
	assert.Equal(t, pjdomain.WallEventAbsorbed, events[1].EventType)
	assert.InDelta(t, -200.0, events[1].DeltaVolume, 1e-6)

	metrics := pjdomain.ReconcileWallData(w, events, trades)
	assert.InDelta(t, 200.0, metrics.AbsorbedVolume, 1e-6)

	// 3. Flap 1: 800 -> 1200 (WALL_RESIZED, delta=400)
	ob3 := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 3,
		Bids: []shared.OrderBookEntry{
			{Price: 3000.0, Volume: 0.1},
			{Price: 2990.0, Volume: 1200.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 3001.0, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob3, now.Add(2*time.Second))

	events = depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 3)
	assert.Equal(t, pjdomain.WallEventResized, events[2].EventType)
	assert.InDelta(t, 400.0, events[2].DeltaVolume, 1e-6)

	metrics = pjdomain.ReconcileWallData(w, events, trades)
	assert.Equal(t, 1, metrics.ResizeCount)
	assert.InDelta(t, 200.0, metrics.AbsorbedVolume, 1e-6)
}

func TestWallDetector_TradeVerifiedAbsorptionVsMakerPull(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      1000.0,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       0.5,
	}

	contractReader := &mockContractReader{}
	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. Initial Wall: 1000 volume at 100.0 (WALL_BORN)
	ob1 := &shared.OrderBook{
		Symbol:  "DOGEUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 1000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob1, now)

	activeWall, found := depthStore.GetActiveWall("DOGEUSDT", shared.SideOpenLong)
	require.True(t, found)
	wallID := activeWall.ID

	// Case A: Phantom Maker Pull (Volume drops 1000 -> 700 with 0 trades executed)
	ob2 := &shared.OrderBook{
		Symbol:  "DOGEUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 700.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob2, now.Add(time.Second))

	events := depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 2)
	assert.Equal(t, pjdomain.WallEventResized, events[1].EventType)
	assert.InDelta(t, -300.0, events[1].DeltaVolume, 1e-6)

	metricsPull := pjdomain.ReconcileWallData(activeWall, events, nil)
	assert.InDelta(t, 0.0, metricsPull.AbsorbedVolume, 1e-6)
	assert.InDelta(t, 300.0, metricsPull.PulledVolume, 1e-6)

	// Case B: Partial Trade Absorption + Partial Maker Pull
	// Volume drops 700 -> 300 (total drop = 400), but trades only equal 150
	trades := []shared.PublicTrade{
		{
			Symbol:    "DOGEUSDT",
			Price:     100.0,
			Volume:    150.0,
			Side:      shared.SideOpenShort, // Taker Sell hitting bid
			Timestamp: now.Add(2 * time.Second),
		},
	}
	depthStore.RecordPublicTrades("DOGEUSDT", trades)
	ob3 := &shared.OrderBook{
		Symbol:  "DOGEUSDT",
		Version: 3,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 300.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 0.1},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob3, now.Add(2*time.Second))

	events = depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 4)
	assert.Equal(t, pjdomain.WallEventAbsorbed, events[2].EventType)
	assert.Equal(t, pjdomain.WallEventWeakened, events[3].EventType)
	assert.InDelta(t, -400.0, events[2].DeltaVolume, 1e-6)

	// Reconcile Wall Data computes accurate trade absorption vs maker pull
	wallTrades := depthStore.GetTradesForWall("DOGEUSDT", 100.0, shared.SideOpenShort, now, now.Add(3*time.Second))
	metricsCombined := pjdomain.ReconcileWallData(activeWall, events, wallTrades)
	assert.InDelta(t, 150.0, metricsCombined.AbsorbedVolume, 1e-6)
	assert.InDelta(t, 550.0, metricsCombined.PulledVolume, 1e-6) // Total drop 700, 150 absorbed -> 550 pulled
	assert.InDelta(t, 700.0, metricsCombined.TotalDropVolume, 1e-6)
}

func TestWallDetector_SpotSkipsAskWalls(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{}

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0,
		MinLifespan:        types.Duration(0),
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	// 1. Spot detector
	spotDetector := application.NewWallDetector("toobit_spot", cfg, depthStore, contractReader, bus, logger)

	// 2. OrderBook with both a Bid wall and an Ask wall
	now := time.Now()
	ob := &shared.OrderBook{
		Symbol:  "DOGEUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 0.1000, Volume: 100.0},
			{Price: 0.0999, Volume: 100.0},
			{Price: 0.0998, Volume: 100.0},
			{Price: 0.0997, Volume: 100.0},
			{Price: 0.0996, Volume: 100.0},
			{Price: 0.0995, Volume: 1000000.0}, // Bid wall (1M doge = $99.5k)
		},
		Asks: []shared.OrderBookEntry{
			{Price: 0.1001, Volume: 100.0},
			{Price: 0.1002, Volume: 100.0},
			{Price: 0.1003, Volume: 100.0},
			{Price: 0.1004, Volume: 100.0},
			{Price: 0.1005, Volume: 100.0},
			{Price: 0.1006, Volume: 1000000.0}, // Ask wall (1M doge = $100.6k)
		},
	}

	detectedSpot := spotDetector.ProcessOrderBook(context.Background(), ob, now)
	require.Len(t, detectedSpot, 1, "Spot should only detect Bid wall (Long)")
	assert.Equal(t, shared.SideOpenLong, detectedSpot[0].Side)

	// 3. Futures detector on the same orderbook
	depthStoreFutures := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	futuresDetector := application.NewWallDetector("toobit_futures", cfg, depthStoreFutures, contractReader, bus, logger)

	detectedFutures := futuresDetector.ProcessOrderBook(context.Background(), ob, now)
	require.Len(t, detectedFutures, 2, "Futures should detect both Bid wall (Long) and Ask wall (Short)")
	assert.Equal(t, shared.SideOpenLong, detectedFutures[0].Side)
	assert.Equal(t, shared.SideOpenShort, detectedFutures[1].Side)
}

func TestWallDetector_RelativeRatioFilter(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{}

	// Require MinRelativeRatio >= 15x
	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      20000.0,
		MinRelativeRatio:   15.0,
		NeighborhoodLevels: 5,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// Case A: Notional is $50,000 (meets $20k) but surrounding levels are 2000 volume ($20,000 each)
	// Ratio = 5000 / 2000 = 2.5x < 15.0x -> REJECTED
	obLowRatio := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 10.0, Volume: 2000.0},
			{Price: 9.99, Volume: 2000.0},
			{Price: 9.98, Volume: 5000.0}, // $50,000 notional, but only 2.5x nearby
			{Price: 9.97, Volume: 2000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 10.01, Volume: 10.0},
		},
	}
	wallsLow := detector.ProcessOrderBook(context.Background(), obLowRatio, now)
	assert.Empty(t, wallsLow, "Wall with low relative ratio (<15x) must be rejected")

	// Case B: Notional is $50,000 and surrounding levels are 100 volume ($1,000 each)
	// Ratio = 5000 / 100 = 50.0x >= 15.0x -> ACCEPTED
	obHighRatio := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 10.0, Volume: 100.0},
			{Price: 9.99, Volume: 100.0},
			{Price: 9.98, Volume: 5000.0}, // $50,000 notional, 50x nearby
			{Price: 9.97, Volume: 100.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 10.01, Volume: 10.0},
		},
	}
	wallsHigh := detector.ProcessOrderBook(context.Background(), obHighRatio, now)
	require.Len(t, wallsHigh, 1)
	assert.Equal(t, 9.98, wallsHigh[0].Price)
	assert.GreaterOrEqual(t, wallsHigh[0].RelativeRatio, 15.0)
}

func TestWallDetector_ConfigurableNeighborhoodLevels(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{}

	// Test with neighborhoodLevels = 2 (considers only +-2 levels around wall)
	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      10000.0,
		MinRelativeRatio:   10.0,
		NeighborhoodLevels: 2,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	ob := &shared.OrderBook{
		Symbol:  "ETHUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 10.0},  // idx 0 (within +-2 of idx 2)
			{Price: 99.9, Volume: 10.0},   // idx 1 (within +-2 of idx 2)
			{Price: 99.8, Volume: 1000.0}, // idx 2: Wall ($100k)
			{Price: 99.7, Volume: 10.0},   // idx 3 (within +-2 of idx 2)
			{Price: 99.6, Volume: 10.0},   // idx 4 (within +-2 of idx 2)
			{Price: 99.5, Volume: 800.0},  // idx 5 (outside +-2, should NOT skew average)
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 10.0},
		},
	}

	detected := detector.ProcessOrderBook(context.Background(), ob, now)
	require.Len(t, detected, 1)
	assert.Equal(t, 99.8, detected[0].Price)
	// Local window average is 10.0, ratio is 1000 / 10 = 100x
	assert.InDelta(t, 100.0, detected[0].RelativeRatio, 1.0)
}

func TestWallDetector_FlappingAsDisappearAndFreshWall(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{}

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      10000.0,
		MinRelativeRatio:   10.0,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	obActive := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 10.0},
			{Price: 99.0, Volume: 1000.0}, // Wall
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 10.0},
		},
	}

	obGone := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 10.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 10.0},
		},
	}

	// 1. T=0s: Wall detected (WALL_BORN)
	walls0 := detector.ProcessOrderBook(context.Background(), obActive, now)
	require.Len(t, walls0, 1)
	initialWallID := walls0[0].ID

	// 2. T=1s: Wall vanishes -> Immediately finalized as WALL_DISAPPEARED
	walls1 := detector.ProcessOrderBook(context.Background(), obGone, now.Add(1*time.Second))
	assert.Empty(t, walls1)

	events := depthStore.GetWallEventStream(initialWallID)
	require.Len(t, events, 2)
	assert.Equal(t, pjdomain.WallEventBorn, events[0].EventType)
	assert.Equal(t, pjdomain.WallEventDisappeared, events[1].EventType)

	// 3. T=2s: Wall returns at exact same price -> Must be a FRESH wall with a new ID and Age=0
	obReturned := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 3,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 10.0},
			{Price: 99.0, Volume: 1100.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 10.0},
		},
	}
	walls2 := detector.ProcessOrderBook(context.Background(), obReturned, now.Add(2*time.Second))
	require.Len(t, walls2, 1)
	assert.NotEqual(t, initialWallID, walls2[0].ID, "Returning wall must receive a fresh ID (no spoof age inheritance)")
	assert.Equal(t, 0*time.Second, walls2[0].GetAgeAt(now.Add(2*time.Second)))
}

func TestWallDetector_PriceShiftAsDisappearAndFreshWall(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{
		contracts: map[string]*store.ContractData{
			"BTCUSDT": {
				Symbol:       "BTCUSDT",
				PriceUnit:    0.01,
				ContractSize: 1.0,
			},
		},
	}

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:      10000.0,
		MinRelativeRatio:   10.0,
		MaxWallDistancePct: 1.0,
		MaxSpreadPct:       1.0,
	}

	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. T=0s: Wall at 100.00
	ob1 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 100.05, Volume: 10.0},
			{Price: 100.00, Volume: 1000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.10, Volume: 10.0},
		},
	}
	walls1 := detector.ProcessOrderBook(context.Background(), ob1, now)
	require.Len(t, walls1, 1)
	origID := walls1[0].ID

	// 2. T=1s: Wall moves to 100.01 -> Old wall at 100.00 DISAPPEARS, new wall at 100.01 is BORN
	ob2 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 100.05, Volume: 10.0},
			{Price: 100.01, Volume: 980.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.10, Volume: 10.0},
		},
	}
	walls2 := detector.ProcessOrderBook(context.Background(), ob2, now.Add(time.Second))
	require.Len(t, walls2, 1)
	assert.NotEqual(t, origID, walls2[0].ID, "Moved wall must receive a fresh ID")
	assert.Equal(t, 100.01, walls2[0].Price)

	// Old wall event stream ended with DISAPPEARED
	oldEvents := depthStore.GetWallEventStream(origID)
	require.Len(t, oldEvents, 2)
	assert.Equal(t, pjdomain.WallEventBorn, oldEvents[0].EventType)
	assert.Equal(t, pjdomain.WallEventDisappeared, oldEvents[1].EventType)
}

func TestWallDetector_PriceApproached(t *testing.T) {
	t.Parallel()

	logger := slog.Default()
	bus := eventbus.New(logger)
	depthStore := pjstore.NewDepthStore(10*time.Minute, 1*time.Minute)
	contractReader := &mockContractReader{}

	cfg := pjdomain.WallDetectorConfig{
		MinVolumeUSDT:              10000.0,
		MinRelativeRatio:           10.0,
		PriceApproachedDistancePct: 0.1, // <= 0.1% distance triggers event
		MaxWallDistancePct:         1.0,
		MaxSpreadPct:               1.0,
	}

	detector := application.NewWallDetector("toobit", cfg, depthStore, contractReader, bus, logger)
	now := time.Now()

	// 1. T=0s: Wall at 99.5 (dist = (100 - 99.5)/100 = 0.5% > 0.1%)
	ob1 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 1,
		Bids: []shared.OrderBookEntry{
			{Price: 100.0, Volume: 10.0},
			{Price: 99.5, Volume: 1000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 100.1, Volume: 10.0},
		},
	}
	walls1 := detector.ProcessOrderBook(context.Background(), ob1, now)
	require.Len(t, walls1, 1)
	wallID := walls1[0].ID

	// 2. T=1s: Market sells into bids, Best Bid moves down to 99.55 (dist = (99.55 - 99.5)/99.55 = 0.05% <= 0.1%)
	ob2 := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 2,
		Bids: []shared.OrderBookEntry{
			{Price: 99.55, Volume: 10.0},
			{Price: 99.50, Volume: 1000.0},
		},
		Asks: []shared.OrderBookEntry{
			{Price: 99.60, Volume: 10.0},
		},
	}
	detector.ProcessOrderBook(context.Background(), ob2, now.Add(time.Second))

	events := depthStore.GetWallEventStream(wallID)
	require.Len(t, events, 2)
	assert.Equal(t, pjdomain.WallEventBorn, events[0].EventType)
	assert.Equal(t, pjdomain.WallEventPriceApproached, events[1].EventType)
}
