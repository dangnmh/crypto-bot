package store_test

import (
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/store"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDepthStore_ActiveWallsAndDepth(t *testing.T) {
	t.Parallel()

	ds := store.NewDepthStore(10*time.Minute, 1*time.Minute)

	wall := pjdomain.Wall{
		Symbol: "BTCUSDT",
		Side:   shared.SideOpenLong,
		Price:  60000.0,
		Volume: 150.0,
	}

	ds.SaveActiveWall(wall)

	got, found := ds.GetActiveWall("BTCUSDT", shared.SideOpenLong)
	require.True(t, found)
	assert.Equal(t, 60000.0, got.Price)
	assert.Equal(t, 150.0, got.Volume)

	_, foundShort := ds.GetActiveWall("BTCUSDT", shared.SideOpenShort)
	assert.False(t, foundShort)

	// Save depth
	ob := &shared.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 100,
		Bids:    []shared.OrderBookEntry{{Price: 60000.0, Volume: 150.0}},
	}
	ds.SaveDepthSnapshot("BTCUSDT", ob)

	gotOB, foundOB := ds.GetLatestDepth("BTCUSDT")
	require.True(t, foundOB)
	assert.Equal(t, int64(100), gotOB.Version)

	// Delete wall
	ds.DeleteActiveWall("BTCUSDT", shared.SideOpenLong)
	_, foundAfterDelete := ds.GetActiveWall("BTCUSDT", shared.SideOpenLong)
	assert.False(t, foundAfterDelete)
}

func TestDepthStore_HistoryTracking(t *testing.T) {
	t.Parallel()

	ds := store.NewDepthStore(10*time.Minute, 1*time.Minute)
	now := time.Now()

	// Record 2 pulls within window, 1 pull outside 1h
	ds.RecordWallPull("BTCUSDT", 60000.0, now.Add(-10*time.Minute))
	ds.RecordWallPull("BTCUSDT", 60000.0, now.Add(-30*time.Minute))
	ds.RecordWallPull("BTCUSDT", 60000.0, now.Add(-90*time.Minute))

	// Record 1 fill within window
	ds.RecordWallFill("BTCUSDT", 60000.0, now.Add(-5*time.Minute))

	hist := ds.GetWallHistory("BTCUSDT", 60000.0, 1*time.Hour, now)
	assert.Equal(t, 2, hist.PullCountIn1h)
	assert.Equal(t, 3, hist.TotalPulls)
	assert.Equal(t, 1, hist.FillCountIn1h)
	assert.Equal(t, 1, hist.TotalFills)
}

func TestDepthStore_WallEventJournal(t *testing.T) {
	t.Parallel()

	ds := store.NewDepthStore(10*time.Minute, 1*time.Minute)
	now := time.Now()

	wallID := "test-wall-journal-1"

	evt1 := pjdomain.WallEvent{
		WallID:    wallID,
		Seq:       1,
		EventType: pjdomain.WallEventBorn,
		Volume:    100.0,
		Timestamp: now,
	}
	evt2 := pjdomain.WallEvent{
		WallID:    wallID,
		Seq:       2,
		EventType: pjdomain.WallEventMatured,
		Volume:    100.0,
		Timestamp: now.Add(2 * time.Second),
	}

	ds.AppendWallEvent(wallID, evt1)
	ds.AppendWallEvent(wallID, evt2)

	stream := ds.GetWallEventStream(wallID)
	require.Len(t, stream, 2)
	assert.Equal(t, int64(1), stream[0].Seq)
	assert.Equal(t, pjdomain.WallEventBorn, stream[0].EventType)
	assert.Equal(t, int64(2), stream[1].Seq)
	assert.Equal(t, pjdomain.WallEventMatured, stream[1].EventType)

	// Clear events
	ds.ClearWallEvents(wallID)
	assert.Nil(t, ds.GetWallEventStream(wallID))
}

func TestDepthStore_PublicTrades(t *testing.T) {
	t.Parallel()

	ds := store.NewDepthStore(10*time.Minute, 1*time.Minute)
	now := time.Now()

	symbol := "BTCUSDT"
	trades := []shared.PublicTrade{
		{
			Symbol:    symbol,
			Price:     60000.0,
			Volume:    10.0,
			Side:      shared.SideOpenShort, // Taker Sell (hitting bid)
			Timestamp: now,
		},
		{
			Symbol:    symbol,
			Price:     60000.0,
			Volume:    15.0,
			Side:      shared.SideOpenShort, // Taker Sell
			Timestamp: now.Add(time.Second),
		},
		{
			Symbol:    symbol,
			Price:     60001.0,
			Volume:    5.0,
			Side:      shared.SideOpenLong, // Taker Buy (hitting ask)
			Timestamp: now.Add(2 * time.Second),
		},
	}

	ds.RecordPublicTrades(symbol, trades)

	// Check GetTradedVolume (non-consuming)
	volBid := ds.GetTradedVolume(symbol, 60000.0, shared.SideOpenShort)
	assert.Equal(t, 25.0, volBid)

	volAsk := ds.GetTradedVolume(symbol, 60001.0, shared.SideOpenLong)
	assert.Equal(t, 5.0, volAsk)

	volWrongSide := ds.GetTradedVolume(symbol, 60000.0, shared.SideOpenLong)
	assert.Equal(t, 0.0, volWrongSide)

	// Check GetTradesForWall (non-consuming window query)
	tradesForWall := ds.GetTradesForWall(symbol, 60000.0, shared.SideOpenShort, now, now.Add(time.Second))
	assert.Len(t, tradesForWall, 2)
	assert.Equal(t, 10.0, tradesForWall[0].Volume)
	assert.Equal(t, 15.0, tradesForWall[1].Volume)

	// Consume trade volume at 60000.0 for Taker Sell
	consumed := ds.ConsumeTradedVolume(symbol, 60000.0, shared.SideOpenShort)
	assert.Equal(t, 25.0, consumed)

	// Second consume should return 0 (already consumed)
	consumedSecond := ds.ConsumeTradedVolume(symbol, 60000.0, shared.SideOpenShort)
	assert.Equal(t, 0.0, consumedSecond)

	// Trade at 60001.0 should still remain
	consumedAsk := ds.ConsumeTradedVolume(symbol, 60001.0, shared.SideOpenLong)
	assert.Equal(t, 5.0, consumedAsk)
}

func TestDepthStore_Volume24h(t *testing.T) {
	t.Parallel()

	ds := store.NewDepthStore(10*time.Minute, 1*time.Minute)

	_, found := ds.GetVolume24h("BTCUSDT")
	assert.False(t, found)

	ds.SaveVolume24h("BTCUSDT", 5000000.0)

	vol, found := ds.GetVolume24h("BTCUSDT")
	assert.True(t, found)
	assert.Equal(t, 5000000.0, vol)
}
