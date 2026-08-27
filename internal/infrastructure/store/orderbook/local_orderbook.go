package orderbook

import (
	"cmp"
	"slices"
	"sync"
	"time"

	"crypto-bot/internal/domain"
)

// LocalOrderBook maintains a high-performance in-memory Level-2 orderbook for a single symbol.
type LocalOrderBook struct {
	symbol        string
	bids          map[float64]float64 // Price -> Volume
	asks          map[float64]float64 // Price -> Volume
	bestBid       float64
	bestAsk       float64
	version       int64
	lastUpdatedAt time.Time
	mu            sync.RWMutex
}

// NewLocalOrderBook creates a new LocalOrderBook instance.
func NewLocalOrderBook(symbol string) *LocalOrderBook {
	return &LocalOrderBook{
		symbol:        symbol,
		bids:          make(map[float64]float64),
		asks:          make(map[float64]float64),
		lastUpdatedAt: time.Now(),
	}
}

// LoadSnapshot replaces the entire orderbook state with a full snapshot.
func (b *LocalOrderBook) LoadSnapshot(ob *domain.OrderBook) {
	if ob == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()

	clear(b.bids)
	clear(b.asks)
	b.bestBid = 0
	b.bestAsk = 0

	for _, entry := range ob.Bids {
		if entry.Volume > 0 && entry.Price > 0 {
			b.bids[entry.Price] = entry.Volume
			if b.bestBid == 0 || entry.Price > b.bestBid {
				b.bestBid = entry.Price
			}
		}
	}
	for _, entry := range ob.Asks {
		if entry.Volume > 0 && entry.Price > 0 {
			b.asks[entry.Price] = entry.Volume
			if b.bestAsk == 0 || entry.Price < b.bestAsk {
				b.bestAsk = entry.Price
			}
		}
	}

	b.version = ob.Version
	if !ob.Timestamp.IsZero() {
		b.lastUpdatedAt = ob.Timestamp
	} else {
		b.lastUpdatedAt = time.Now().UTC()
	}
}

// ApplyDelta updates existing price levels, inserts new ones, or deletes price levels with volume <= 0.
func (b *LocalOrderBook) ApplyDelta(bids, asks []domain.OrderBookEntry, version int64, t time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.bestBid = updatePriceLevels(bids, b.bids, b.bestBid, true)
	b.bestAsk = updatePriceLevels(asks, b.asks, b.bestAsk, false)

	b.version = version
	if !t.IsZero() {
		b.lastUpdatedAt = t
	} else {
		b.lastUpdatedAt = time.Now().UTC()
	}
}

func isBetterPrice(p, best float64, isBid bool) bool {
	if best == 0 {
		return true
	}
	if isBid {
		return p > best
	}
	return p < best
}

func findBestPrice(bookMap map[float64]float64, isBid bool) float64 {
	best := 0.0
	for p := range bookMap {
		if isBetterPrice(p, best, isBid) {
			best = p
		}
	}
	return best
}

func updatePriceLevels(entries []domain.OrderBookEntry, bookMap map[float64]float64, currentBest float64, isBid bool) float64 {
	recompute := false
	best := currentBest

	for _, entry := range entries {
		if entry.Price <= 0 {
			continue
		}
		if entry.Volume <= 0 {
			delete(bookMap, entry.Price)
			if entry.Price == best {
				recompute = true
			}
			continue
		}
		bookMap[entry.Price] = entry.Volume
		if isBetterPrice(entry.Price, best, isBid) {
			best = entry.Price
		}
	}

	if recompute {
		return findBestPrice(bookMap, isBid)
	}
	return best
}

// GetSnapshot extracts a sorted domain.OrderBook snapshot with optional top-N limit.
func (b *LocalOrderBook) GetSnapshot(depthLimit int) *domain.OrderBook {
	b.mu.RLock()
	defer b.mu.RUnlock()

	// 1. Sort Bids descending (highest price first)
	sortedBids := make([]domain.OrderBookEntry, 0, len(b.bids))
	for p, v := range b.bids {
		sortedBids = append(sortedBids, domain.OrderBookEntry{Price: p, Volume: v})
	}
	slices.SortFunc(sortedBids, func(a, b domain.OrderBookEntry) int {
		return cmp.Compare(b.Price, a.Price) // Descending
	})

	// 2. Sort Asks ascending (lowest price first)
	sortedAsks := make([]domain.OrderBookEntry, 0, len(b.asks))
	for p, v := range b.asks {
		sortedAsks = append(sortedAsks, domain.OrderBookEntry{Price: p, Volume: v})
	}
	slices.SortFunc(sortedAsks, func(a, b domain.OrderBookEntry) int {
		return cmp.Compare(a.Price, b.Price) // Ascending
	})

	// 3. Apply optional depth limit
	if depthLimit > 0 {
		if len(sortedBids) > depthLimit {
			sortedBids = sortedBids[:depthLimit]
		}
		if len(sortedAsks) > depthLimit {
			sortedAsks = sortedAsks[:depthLimit]
		}
	}

	return &domain.OrderBook{
		Symbol:    b.symbol,
		Version:   b.version,
		Timestamp: b.lastUpdatedAt,
		Bids:      sortedBids,
		Asks:      sortedAsks,
	}
}

// GetBBO returns the Best Bid and Best Ask prices in O(1) time.
func (b *LocalOrderBook) GetBBO() (bestBid, bestAsk float64, ok bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	if b.bestBid <= 0 || b.bestAsk <= 0 {
		return 0, 0, false
	}
	return b.bestBid, b.bestAsk, true
}

// Version returns the current sequence ID or version of the orderbook.
func (b *LocalOrderBook) Version() int64 {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.version
}

// LastUpdatedAt returns the timestamp of the most recent update.
func (b *LocalOrderBook) LastUpdatedAt() time.Time {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastUpdatedAt
}

// Count returns the number of active price levels in bids and asks.
func (b *LocalOrderBook) Count() (bidCount, askCount int) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.bids), len(b.asks)
}

// Clear wipes all orderbook entries.
func (b *LocalOrderBook) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	clear(b.bids)
	clear(b.asks)
	b.bestBid = 0
	b.bestAsk = 0
	b.version = 0
	b.lastUpdatedAt = time.Now()
}
