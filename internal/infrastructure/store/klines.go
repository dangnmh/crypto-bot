package store

import (
	"crypto-bot/internal/infrastructure/exchange"
	"sync"
)

// KlineBuffer holds a thread-safe ring buffer (or slice) of recent klines.
type KlineBuffer struct {
	mu     sync.RWMutex
	klines []exchange.Kline
	maxLen int
}

func NewKlineBuffer(maxLen int) *KlineBuffer {
	return &KlineBuffer{
		klines: make([]exchange.Kline, 0, maxLen),
		maxLen: maxLen,
	}
}

// Add appends a kline or updates the most recent one if it's the same timeframe.
// For simplicity, we just push and keep the latest maxLen klines.
func (b *KlineBuffer) Add(k exchange.Kline) {
	b.mu.Lock()
	defer b.mu.Unlock()

	n := len(b.klines)
	if n > 0 {
		last := &b.klines[n-1]
		// If same timeframe, update
		if k.Timestamp == last.Timestamp {
			*last = k
			return
		}
		// If older, ignore out-of-order for simplicity, or just append
		if k.Timestamp < last.Timestamp {
			return
		}
	}

	b.klines = append(b.klines, k)
	if len(b.klines) > b.maxLen {
		// shift left
		b.klines = b.klines[1:]
	}
}

// GetKlines returns a copy of the current klines.
func (b *KlineBuffer) GetKlines() []exchange.Kline {
	b.mu.RLock()
	defer b.mu.RUnlock()

	out := make([]exchange.Kline, len(b.klines))
	copy(out, b.klines)
	return out
}
