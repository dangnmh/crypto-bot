package store

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// GlobalStore is the centralized, thread-safe store for all market data.
// Background sync services write data; consumers read from it.
type GlobalStore struct {
	mu        sync.RWMutex
	prices    map[string]*PriceData          // WS push (realtime)
	tickers   map[string]*TickerData         // REST sync (30s)
	contracts map[string]*ContractData       // REST sync (5min)
	funding   map[string]*FundingData        // REST sync (30s per symbol)
	klines    map[string]*KlineBuffer        // WS push + REST initial
	depth     map[string]*exchange.OrderBook // WS push (realtime)
	logger    *slog.Logger

	ready             chan struct{}
	tickerReadyOnce   sync.Once
	contractReadyOnce sync.Once
	fundingReadyOnce  sync.Once
	syncWG            sync.WaitGroup
}

// New creates a new GlobalStore.
func New() *GlobalStore {
	s := &GlobalStore{
		prices:    make(map[string]*PriceData),
		tickers:   make(map[string]*TickerData),
		contracts: make(map[string]*ContractData),
		funding:   make(map[string]*FundingData),
		klines:    make(map[string]*KlineBuffer),
		depth:     make(map[string]*exchange.OrderBook),
		logger:    slog.Default().With("component", "store"),
		ready:     make(chan struct{}),
	}

	s.syncWG.Add(3)
	go func() {
		s.syncWG.Wait()
		close(s.ready)
		s.logger.Info("🟢 Store ready")
	}()

	return s
}

// WaitReady blocks until the first successful data sync completes or context is cancelled.
func (s *GlobalStore) WaitReady(ctx context.Context) {
	select {
	case <-s.ready:
	case <-ctx.Done():
	}
}

func (s *GlobalStore) markTickerReady() {
	s.tickerReadyOnce.Do(func() { s.syncWG.Done() })
}

func (s *GlobalStore) markContractReady() {
	s.contractReadyOnce.Do(func() { s.syncWG.Done() })
}

func (s *GlobalStore) markFundingReady() {
	s.fundingReadyOnce.Do(func() { s.syncWG.Done() })
}

// ──────────────────────────────────────────────────────────────────────
// Price (WS push)
// ──────────────────────────────────────────────────────────────────────.

// WsTickerData represents the data field of a push.ticker WS message.
type WsTickerData struct {
	Symbol      string  `json:"symbol"`
	LastPrice   float64 `json:"lastPrice"`
	FairPrice   float64 `json:"fairPrice"`
	IndexPrice  float64 `json:"indexPrice"`
	Volume24    float64 `json:"volume24"`
	Amount24    float64 `json:"amount24"`
	MaxBidPrice float64 `json:"maxBidPrice"`
	MinAskPrice float64 `json:"minAskPrice"`
	Timestamp   int64   `json:"timestamp"`
	Bid1        float64 `json:"bid1"`
	Ask1        float64 `json:"ask1"`
}

// UpdatePrice writes a price update for a symbol (called by WS client).
func (s *GlobalStore) UpdatePrice(symbol string, data *PriceData) {
	s.mu.Lock()
	s.prices[symbol] = data
	s.mu.Unlock()

	s.logger.Debug("store.UpdatePrice",
		"symbol", symbol,
		"lastPrice", data.LastPrice,
		"bid", data.BestBid,
		"ask", data.BestAsk,
	)
}

// UpdatePriceFromWsTicker parses a WS ticker push message and updates the price store.
func (s *GlobalStore) UpdatePriceFromWsTicker(symbol string, raw json.RawMessage) {
	var ticker WsTickerData
	if err := json.Unmarshal(raw, &ticker); err != nil {
		return
	}

	pd := &PriceData{
		Symbol:    symbol,
		LastPrice: ticker.LastPrice,
		BestBid:   ticker.Bid1,
		BestAsk:   ticker.Ask1,
		FairPrice: ticker.FairPrice,
		Volume24:  ticker.Volume24,
		UpdatedAt: time.Now(),
	}

	// Fallback if bid1/ask1 are 0
	if pd.BestBid == 0 && ticker.MaxBidPrice > 0 {
		pd.BestBid = ticker.MaxBidPrice
	}
	if pd.BestAsk == 0 && ticker.MinAskPrice > 0 {
		pd.BestAsk = ticker.MinAskPrice
	}

	s.mu.Lock()
	s.prices[symbol] = pd
	s.mu.Unlock()
}

// GetPrice returns the latest price for a symbol.
// Returns error if data is stale (older than maxAge) or not found.
func (s *GlobalStore) GetPrice(symbol string, maxAge time.Duration) (*PriceData, error) {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no price data for %s", symbol)
	}

	age := time.Since(pd.UpdatedAt)
	if age > maxAge {
		return pd, fmt.Errorf("price data stale for %s (age: %v)", symbol, age)
	}

	return pd, nil
}

// GetBestBidAsk returns the best bid and ask for a symbol.
func (s *GlobalStore) GetBestBidAsk(symbol string) (bid, ask float64, err error) {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return 0, 0, fmt.Errorf("no price data for %s", symbol)
	}

	if pd.BestBid == 0 || pd.BestAsk == 0 {
		return pd.BestBid, pd.BestAsk, fmt.Errorf("incomplete bid/ask for %s", symbol)
	}

	return pd.BestBid, pd.BestAsk, nil
}

// PriceAge returns how old the price data is for a symbol.
func (s *GlobalStore) PriceAge(symbol string) time.Duration {
	s.mu.RLock()
	pd, ok := s.prices[symbol]
	s.mu.RUnlock()

	if !ok {
		return time.Hour // very old if not found
	}
	return time.Since(pd.UpdatedAt)
}

// ──────────────────────────────────────────────────────────────────────
// Ticker (REST sync)
// ──────────────────────────────────────────────────────────────────────.

// GetTicker returns the latest ticker data for a symbol.
func (s *GlobalStore) GetTicker(symbol string) (*TickerData, error) {
	s.mu.RLock()
	td, ok := s.tickers[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no ticker data for %s", symbol)
	}
	return td, nil
}

// GetAllTickers returns all ticker data as a slice.
func (s *GlobalStore) GetAllTickers() []*TickerData {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result := make([]*TickerData, 0, len(s.tickers))
	for _, td := range s.tickers {
		result = append(result, td)
	}
	return result
}

// ──────────────────────────────────────────────────────────────────────
// Contract (REST sync)
// ──────────────────────────────────────────────────────────────────────.

// GetContract returns the contract specification for a symbol.
func (s *GlobalStore) GetContract(symbol string) (*ContractData, error) {
	s.mu.RLock()
	cd, ok := s.contracts[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no contract data for %s", symbol)
	}
	return cd, nil
}

// ──────────────────────────────────────────────────────────────────────
// Funding (REST sync)
// ──────────────────────────────────────────────────────────────────────.

// GetFunding returns the funding rate detail for a symbol.
func (s *GlobalStore) GetFunding(symbol string) (*FundingData, error) {
	s.mu.RLock()
	fd, ok := s.funding[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("no funding data for %s", symbol)
	}
	return fd, nil
}

// GetSettleTime returns the next settlement time for a symbol.
func (s *GlobalStore) GetSettleTime(symbol string) (time.Time, error) {
	s.mu.RLock()
	// Try funding data first (more specific, per-symbol)
	fd, ok := s.funding[symbol]
	if ok && fd.NextSettleTime > 0 {
		s.mu.RUnlock()
		return time.UnixMilli(fd.NextSettleTime), nil
	}

	// Fallback to ticker data
	td, ok := s.tickers[symbol]
	if ok && td.NextSettleTime > 0 {
		s.mu.RUnlock()
		return time.UnixMilli(td.NextSettleTime), nil
	}
	s.mu.RUnlock()

	return time.Time{}, fmt.Errorf("no settle time for %s", symbol)
}

// ──────────────────────────────────────────────────────────────────────
// Klines (REST initial + WS sync)
// ──────────────────────────────────────────────────────────────────────.

// InitKlines initializes the kline buffer for a symbol with initial data.
func (s *GlobalStore) InitKlines(symbol string, maxLen int, initial []exchange.Kline) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.klines[symbol]; !ok {
		s.klines[symbol] = NewKlineBuffer(maxLen)
	}

	buf := s.klines[symbol]
	for _, k := range initial {
		buf.Add(k)
	}
}

// AddKline adds a single kline update (usually from WS).
func (s *GlobalStore) AddKline(symbol string, k exchange.Kline) {
	s.mu.RLock()
	buf, ok := s.klines[symbol]
	s.mu.RUnlock()

	if ok {
		buf.Add(k)
	}
}

// GetKlines returns the current klines for a symbol.
func (s *GlobalStore) GetKlines(symbol string) []exchange.Kline {
	s.mu.RLock()
	buf, ok := s.klines[symbol]
	s.mu.RUnlock()

	if !ok {
		return nil
	}
	return buf.GetKlines()
}

// ──────────────────────────────────────────────────────────────────────
// Order Book Depth
// ──────────────────────────────────────────────────────────────────────.

// UpdateDepth overwrites the order book depth for a symbol.
func (s *GlobalStore) UpdateDepth(symbol string, ob *exchange.OrderBook) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.depth[symbol] = ob
}

// GetDepth retrieves the current order book depth for a symbol.
func (s *GlobalStore) GetDepth(symbol string) (*exchange.OrderBook, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ob, exists := s.depth[symbol]
	if !exists || ob == nil {
		return nil, fmt.Errorf("no depth data for symbol %s", symbol)
	}
	// Return a copy to avoid race conditions when reading while WS updates it
	obCopy := &exchange.OrderBook{
		Symbol:  ob.Symbol,
		Version: ob.Version,
		Asks:    make([]exchange.OrderBookEntry, len(ob.Asks)),
		Bids:    make([]exchange.OrderBookEntry, len(ob.Bids)),
	}
	copy(obCopy.Asks, ob.Asks)
	copy(obCopy.Bids, ob.Bids)
	return obCopy, nil
}
