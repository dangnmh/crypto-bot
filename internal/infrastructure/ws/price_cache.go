package ws

import (
	"crypto-bot/internal/infrastructure/store"
	"sync"
	"time"
)

// PriceCache is a shared, thread-safe utility to cache and merge ticker and depth price data.
type PriceCache struct {
	mu     sync.Mutex
	prices map[string]*store.PriceData
}

// NewPriceCache creates a new PriceCache.
func NewPriceCache() *PriceCache {
	return &PriceCache{
		prices: make(map[string]*store.PriceData),
	}
}

// UpdateTicker updates ticker metrics (LastPrice, FairPrice, Volume24) for a symbol.
// If the entry does not exist, it initializes it.
func (c *PriceCache) UpdateTicker(symbol string, lastPrice, fairPrice, volume24 float64) *store.PriceData {
	c.mu.Lock()
	defer c.mu.Unlock()

	pd, exists := c.prices[symbol]
	if !exists {
		pd = &store.PriceData{Symbol: symbol}
		c.prices[symbol] = pd
	}

	if lastPrice > 0 {
		pd.LastPrice = lastPrice
		if pd.BestBid == 0 {
			pd.BestBid = lastPrice
		}
		if pd.BestAsk == 0 {
			pd.BestAsk = lastPrice
		}
	}
	if fairPrice > 0 {
		pd.FairPrice = fairPrice
	} else if pd.BestBid > 0 && pd.BestAsk > 0 {
		pd.FairPrice = 0.5 * (pd.BestBid + pd.BestAsk)
	}
	if volume24 > 0 {
		pd.Volume24 = volume24
	}
	if pd.LastPrice == 0 && pd.FairPrice > 0 {
		pd.LastPrice = pd.FairPrice
	}
	pd.UpdatedAt = time.Now()

	snapshot := *pd
	return &snapshot
}

// UpdateDepth updates order book depth metrics (BestBid, BestAsk) for a symbol.
// If the entry does not exist, it initializes it.
func (c *PriceCache) UpdateDepth(symbol string, bid, ask float64) *store.PriceData {
	c.mu.Lock()
	defer c.mu.Unlock()

	pd, exists := c.prices[symbol]
	if !exists {
		pd = &store.PriceData{Symbol: symbol}
		c.prices[symbol] = pd
	}

	if bid > 0 {
		pd.BestBid = bid
	}
	if ask > 0 {
		pd.BestAsk = ask
	}
	pd.UpdatedAt = time.Now()

	snapshot := *pd
	return &snapshot
}

// UpdateDepthAndMidPrice updates order book depth metrics (BestBid, BestAsk) and calculates FairPrice as the mid-price.
func (c *PriceCache) UpdateDepthAndMidPrice(symbol string, bid, ask float64) *store.PriceData {
	c.mu.Lock()
	defer c.mu.Unlock()

	pd, exists := c.prices[symbol]
	if !exists {
		pd = &store.PriceData{Symbol: symbol}
		c.prices[symbol] = pd
	}

	if bid > 0 {
		pd.BestBid = bid
	}
	if ask > 0 {
		pd.BestAsk = ask
	}
	if pd.BestBid > 0 && pd.BestAsk > 0 {
		pd.FairPrice = 0.5 * (pd.BestBid + pd.BestAsk)
	}
	pd.UpdatedAt = time.Now()

	snapshot := *pd
	return &snapshot
}

// Get returns a copy of the current cached price data for a symbol.
func (c *PriceCache) Get(symbol string) (*store.PriceData, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pd, exists := c.prices[symbol]
	if !exists {
		return nil, false
	}
	snapshot := *pd
	return &snapshot, true
}
