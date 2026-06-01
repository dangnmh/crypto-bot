package store

import (
	"context"
	"log/slog"
	"sync"

	"crypto-bot/internal/domain"
)

// KlineStore manages historical candlestick data buffers.
type KlineStore struct {
	klines map[string]*KlineBuffer
	mu     sync.RWMutex
	logger *slog.Logger
}

// NewKlineStore creates a new KlineStore.
func NewKlineStore(log *slog.Logger) *KlineStore {
	return &KlineStore{
		klines: make(map[string]*KlineBuffer),
		logger: log.With("component", "kline_store"),
	}
}

// InitKlines initializes the kline buffer for a symbol with historical REST data.
func (s *KlineStore) InitKlines(symbol string, maxLen int, initial []domain.Kline) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf := NewKlineBuffer(maxLen)

	for _, k := range initial {
		buf.Add(k)
	}

	s.klines[symbol] = buf
	s.logger.Debug("store.InitKlines", slog.String("symbol", symbol), slog.Int("count", len(buf.klines)))
}

// AddKline adds a new kline (usually from WS push) to the buffer.
func (s *KlineStore) AddKline(symbol string, k domain.Kline) {
	s.mu.Lock()
	defer s.mu.Unlock()

	buf, ok := s.klines[symbol]
	if !ok {
		buf = NewKlineBuffer(50)
		s.klines[symbol] = buf
	}

	buf.Add(k)
}

// GetKlines returns a copy of the klines buffer for a symbol.
func (s *KlineStore) GetKlines(_ context.Context, symbol string) []domain.Kline {
	s.mu.RLock()
	defer s.mu.RUnlock()

	buf, ok := s.klines[symbol]
	if !ok {
		return nil
	}

	res := make([]domain.Kline, len(buf.klines))
	copy(res, buf.klines)
	return res
}
