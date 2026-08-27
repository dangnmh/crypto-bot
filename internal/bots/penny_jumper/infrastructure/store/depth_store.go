package store

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	shared "crypto-bot/internal/domain"

	"github.com/patrickmn/go-cache"
)

// WallHistoryRecord holds historical pull and fill counts for a price level.
type WallHistoryRecord struct {
	PullCountIn1h int
	FillCountIn1h int
	TotalPulls    int
	TotalFills    int
}

// DepthStore manages orderbook depth snapshots, active detected walls, and historical pull/fill events using go-cache.
type DepthStore struct {
	cache *cache.Cache
	mu    sync.RWMutex
}

// NewDepthStore creates a new DepthStore backed by go-cache.
func NewDepthStore(defaultTTL, cleanupInterval time.Duration) *DepthStore {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Minute
	}
	if cleanupInterval <= 0 {
		cleanupInterval = 5 * time.Minute
	}
	return &DepthStore{
		cache: cache.New(defaultTTL, cleanupInterval),
	}
}

func depthKey(symbol string) string {
	return fmt.Sprintf("depth:%s", symbol)
}

func activeWallKey(symbol string, side shared.Side) string {
	return fmt.Sprintf("wall:active:%s:%d", symbol, side)
}

func historyKey(symbol string, price float64) string {
	return fmt.Sprintf("wall:hist:%s:%s", symbol, strconv.FormatFloat(price, 'f', -1, 64))
}

// SaveDepthSnapshot stores the latest orderbook depth snapshot for a symbol.
func (s *DepthStore) SaveDepthSnapshot(symbol string, ob *shared.OrderBook) {
	if ob == nil {
		return
	}
	s.cache.Set(depthKey(symbol), ob, cache.DefaultExpiration)
}

// GetLatestDepth retrieves the latest orderbook depth snapshot for a symbol.
func (s *DepthStore) GetLatestDepth(symbol string) (*shared.OrderBook, bool) {
	val, found := s.cache.Get(depthKey(symbol))
	if !found {
		return nil, false
	}
	ob, ok := val.(*shared.OrderBook)
	if !ok {
		return nil, false
	}
	return ob, true
}

// SaveActiveWall stores the active wall for a symbol and side.
func (s *DepthStore) SaveActiveWall(wall pjdomain.Wall) {
	s.cache.Set(activeWallKey(wall.Symbol, wall.Side), wall, cache.DefaultExpiration)
}

// GetActiveWall retrieves the active wall for a symbol and side.
func (s *DepthStore) GetActiveWall(symbol string, side shared.Side) (*pjdomain.Wall, bool) {
	val, found := s.cache.Get(activeWallKey(symbol, side))
	if !found {
		return nil, false
	}
	wall, ok := val.(pjdomain.Wall)
	if !ok {
		return nil, false
	}
	return &wall, true
}

// DeleteActiveWall removes the active wall for a symbol and side.
func (s *DepthStore) DeleteActiveWall(symbol string, side shared.Side) {
	s.cache.Delete(activeWallKey(symbol, side))
}

type pullFillEvent struct {
	isPull    bool
	timestamp time.Time
}

// RecordWallPull records a wall pull/cancellation event at the given price.
func (s *DepthStore) RecordWallPull(symbol string, price float64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := historyKey(symbol, price)
	var events []pullFillEvent
	if val, found := s.cache.Get(k); found {
		if existing, ok := val.([]pullFillEvent); ok {
			events = existing
		}
	}
	events = append(events, pullFillEvent{isPull: true, timestamp: now})
	s.cache.Set(k, events, 2*time.Hour)
}

// RecordWallFill records a wall fill event at the given price.
func (s *DepthStore) RecordWallFill(symbol string, price float64, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := historyKey(symbol, price)
	var events []pullFillEvent
	if val, found := s.cache.Get(k); found {
		if existing, ok := val.([]pullFillEvent); ok {
			events = existing
		}
	}
	events = append(events, pullFillEvent{isPull: false, timestamp: now})
	s.cache.Set(k, events, 2*time.Hour)
}

// GetWallHistory returns aggregate pull and fill statistics within the specified time window.
func (s *DepthStore) GetWallHistory(symbol string, price float64, window time.Duration, now time.Time) WallHistoryRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := historyKey(symbol, price)
	val, found := s.cache.Get(k)
	if !found {
		return WallHistoryRecord{}
	}

	events, ok := val.([]pullFillEvent)
	if !ok {
		return WallHistoryRecord{}
	}

	cutoff := now.Add(-window)
	rec := WallHistoryRecord{}

	for _, e := range events {
		if e.isPull {
			rec.TotalPulls++
			if e.timestamp.After(cutoff) {
				rec.PullCountIn1h++
			}
		} else {
			rec.TotalFills++
			if e.timestamp.After(cutoff) {
				rec.FillCountIn1h++
			}
		}
	}

	return rec
}

func wallEventsKey(wallID string) string {
	return fmt.Sprintf("wall:events:%s", wallID)
}

// AppendWallEvent adds an immutable WallEvent to the in-memory journal for the wall.
func (s *DepthStore) AppendWallEvent(wallID string, evt pjdomain.WallEvent) {
	if wallID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	k := wallEventsKey(wallID)
	var events []pjdomain.WallEvent
	if val, found := s.cache.Get(k); found {
		if existing, ok := val.([]pjdomain.WallEvent); ok {
			events = existing
		}
	}
	events = append(events, evt)
	s.cache.Set(k, events, 1*time.Hour)
}

// GetWallEventStream returns the full sequence of events for a wall from in-memory cache.
func (s *DepthStore) GetWallEventStream(wallID string) []pjdomain.WallEvent {
	if wallID == "" {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := wallEventsKey(wallID)
	if val, found := s.cache.Get(k); found {
		if events, ok := val.([]pjdomain.WallEvent); ok {
			result := make([]pjdomain.WallEvent, len(events))
			copy(result, events)
			return result
		}
	}
	return nil
}

// ClearWallEvents deletes in-memory events for a wall.
func (s *DepthStore) ClearWallEvents(wallID string) {
	if wallID == "" {
		return
	}
	s.cache.Delete(wallEventsKey(wallID))
}

func tradesKey(symbol string) string {
	return fmt.Sprintf("trades:%s", symbol)
}

// RecordPublicTrades stores real-time public trade executions in the in-memory tape for symbol.
func (s *DepthStore) RecordPublicTrades(symbol string, trades []shared.PublicTrade) {
	if len(trades) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	k := tradesKey(symbol)
	var existing []shared.PublicTrade
	if val, found := s.cache.Get(k); found {
		if ex, ok := val.([]shared.PublicTrade); ok {
			existing = ex
		}
	}

	existing = append(existing, trades...)
	// Keep at most 1000 recent trades to prevent unbounded growth
	if len(existing) > 1000 {
		existing = existing[len(existing)-1000:]
	}
	s.cache.Set(k, existing, 1*time.Hour)
}

// ConsumeTradedVolume calculates and atomically consumes the executed trade volume matching price and taker side.
func (s *DepthStore) ConsumeTradedVolume(symbol string, price float64, takerSide shared.Side) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := tradesKey(symbol)
	val, found := s.cache.Get(k)
	if !found {
		return 0
	}

	trades, ok := val.([]shared.PublicTrade)
	if !ok || len(trades) == 0 {
		return 0
	}

	var remaining []shared.PublicTrade
	consumedVol := 0.0

	for _, t := range trades {
		matchesPrice := t.Price == price
		matchesSide := (takerSide == 0) || (t.Side == takerSide)

		if matchesPrice && matchesSide {
			consumedVol += t.Volume
		} else {
			remaining = append(remaining, t)
		}
	}

	if len(remaining) > 0 {
		s.cache.Set(k, remaining, 1*time.Hour)
	} else {
		s.cache.Delete(k)
	}

	return consumedVol
}

// GetTradesForWall returns public trades matching the wall price and side within the specified time window.
func (s *DepthStore) GetTradesForWall(symbol string, price float64, takerSide shared.Side, from, to time.Time) []shared.PublicTrade {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := tradesKey(symbol)
	val, found := s.cache.Get(k)
	if !found {
		return nil
	}

	trades, ok := val.([]shared.PublicTrade)
	if !ok || len(trades) == 0 {
		return nil
	}

	var matching []shared.PublicTrade
	for _, t := range trades {
		matchesPrice := t.Price == price
		matchesSide := (takerSide == 0) || (t.Side == takerSide)
		matchesTime := true
		if !from.IsZero() && t.Timestamp.Before(from) {
			matchesTime = false
		}
		if !to.IsZero() && t.Timestamp.After(to) {
			matchesTime = false
		}

		if matchesPrice && matchesSide && matchesTime {
			matching = append(matching, t)
		}
	}
	return matching
}

// GetTradedVolume returns matching traded volume without consuming it.
func (s *DepthStore) GetTradedVolume(symbol string, price float64, takerSide shared.Side) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	k := tradesKey(symbol)
	val, found := s.cache.Get(k)
	if !found {
		return 0
	}

	trades, ok := val.([]shared.PublicTrade)
	if !ok || len(trades) == 0 {
		return 0
	}

	totalVol := 0.0
	for _, t := range trades {
		matchesPrice := t.Price == price
		matchesSide := (takerSide == 0) || (t.Side == takerSide)
		if matchesPrice && matchesSide {
			totalVol += t.Volume
		}
	}
	return totalVol
}
