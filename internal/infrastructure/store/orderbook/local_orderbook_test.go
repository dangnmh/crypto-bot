package orderbook_test

import (
	"sync"
	"testing"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/store/orderbook"

	"github.com/stretchr/testify/assert"
)

func TestLocalOrderBook_SnapshotAndDelta(t *testing.T) {
	t.Parallel()

	ob := orderbook.NewLocalOrderBook("BTCUSDT")

	// 1. Load Snapshot
	snap := &domain.OrderBook{
		Symbol:  "BTCUSDT",
		Version: 100,
		Bids: []domain.OrderBookEntry{
			{Price: 60000.0, Volume: 1.5},
			{Price: 59900.0, Volume: 2.0},
			{Price: 59800.0, Volume: 5.0},
		},
		Asks: []domain.OrderBookEntry{
			{Price: 60100.0, Volume: 0.8},
			{Price: 60200.0, Volume: 1.2},
		},
	}
	ob.LoadSnapshot(snap)

	assert.Equal(t, int64(100), ob.Version())
	bidsCount, asksCount := ob.Count()
	assert.Equal(t, 3, bidsCount)
	assert.Equal(t, 2, asksCount)

	bestBid, bestAsk, ok := ob.GetBBO()
	assert.True(t, ok)
	assert.Equal(t, 60000.0, bestBid)
	assert.Equal(t, 60100.0, bestAsk)

	// 2. Apply Delta: update bid, insert ask, delete bid (volume = 0)
	deltaBids := []domain.OrderBookEntry{
		{Price: 60000.0, Volume: 3.5}, // update
		{Price: 59900.0, Volume: 0.0}, // delete
		{Price: 60050.0, Volume: 0.5}, // insert new top bid
	}
	deltaAsks := []domain.OrderBookEntry{
		{Price: 60100.0, Volume: 0.0}, // delete top ask
		{Price: 60080.0, Volume: 1.0}, // insert new top ask
	}
	ob.ApplyDelta(deltaBids, deltaAsks, 101, time.Now())

	assert.Equal(t, int64(101), ob.Version())

	// Check updated BBO
	bestBid, bestAsk, ok = ob.GetBBO()
	assert.True(t, ok)
	assert.Equal(t, 60050.0, bestBid)
	assert.Equal(t, 60080.0, bestAsk)

	// Check Top N slice
	topSnap := ob.GetSnapshot(2)
	assert.Len(t, topSnap.Bids, 2)
	assert.Equal(t, 60050.0, topSnap.Bids[0].Price)
	assert.Equal(t, 60000.0, topSnap.Bids[1].Price)
	assert.Len(t, topSnap.Asks, 2)
	assert.Equal(t, 60080.0, topSnap.Asks[0].Price)
	assert.Equal(t, 60200.0, topSnap.Asks[1].Price)

	// 3. Clear
	ob.Clear()
	assert.Equal(t, int64(0), ob.Version())
	_, _, ok = ob.GetBBO()
	assert.False(t, ok)
}

func TestLocalOrderBook_ConcurrentReadWrite(t *testing.T) {
	t.Parallel()

	ob := orderbook.NewLocalOrderBook("ETHUSDT")
	var wg sync.WaitGroup

	// Writers
	for i := range 5 {
		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			for j := 1; j <= 50; j++ {
				price := float64(3000 + j + workerID)
				ob.ApplyDelta(
					[]domain.OrderBookEntry{{Price: price, Volume: float64(j)}},
					[]domain.OrderBookEntry{{Price: price + 10, Volume: float64(j)}},
					int64(j),
					time.Now(),
				)
			}
		}(i)
	}

	// Readers
	for range 5 {
		wg.Go(func() {
			for range 50 {
				_ = ob.GetSnapshot(10)
				_, _, _ = ob.GetBBO()
				_ = ob.Version()
			}
		})
	}

	wg.Wait()
	assert.True(t, ob.Version() > 0)
}
