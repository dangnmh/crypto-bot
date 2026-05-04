package timesync

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"
)

// TimeSync continuously synchronizes local time with MEXC server time.
// Uses Exponential Moving Average to smooth out network jitter.
type TimeSync struct {
	client    exchange.Client
	mu        sync.RWMutex
	offset    int64 // server - local (ms)
	latency   int64 // round-trip time (ms)
	lastSync  time.Time
	healthy   bool
	alpha     float64 // EMA smoothing factor
	interval  time.Duration
	logger    *slog.Logger
	ready     chan struct{}
	readyOnce sync.Once
}

// New creates a new TimeSync service.
func New(client exchange.Client, interval time.Duration) *TimeSync {
	return &TimeSync{
		client:   client,
		alpha:    0.3,
		interval: interval,
		logger:   slog.Default().With("component", "timesync"),
		ready:    make(chan struct{}),
	}
}

// WaitReady blocks until the first successful time sync completes or context is cancelled.
func (ts *TimeSync) WaitReady(ctx context.Context) {
	select {
	case <-ts.ready:
	case <-ctx.Done():
	}
}

// Start begins the continuous time synchronization loop.
// Exits when ctx is cancelled.
func (ts *TimeSync) Start(ctx context.Context) {
	ts.logger.Info("⏱️  Starting time sync service...", "interval", ts.interval)
	defer ts.logger.Info("⏱️  Time sync stopped")

	ticker.RunImmediate(ctx, ts.interval, func() bool {
		ts.syncOnce(ctx)
		return true
	})
}

// syncOnce performs a single time sync round.
func (ts *TimeSync) syncOnce(ctx context.Context) {
	localBefore := time.Now().UnixMilli()
	serverTime, err := ts.client.GetServerTime(ctx)
	localAfter := time.Now().UnixMilli()

	if err != nil {
		ts.mu.Lock()
		ts.healthy = false
		ts.mu.Unlock()
		ts.logger.Error("🔴 Time sync failed", "error", err)
		return
	}

	rtt := localAfter - localBefore
	localMid := localBefore + rtt/2
	newOffset := serverTime - localMid

	ts.mu.Lock()
	if ts.lastSync.IsZero() {
		// First sync — use raw value
		ts.offset = newOffset
	} else {
		// EMA smoothing
		ts.offset = int64(float64(ts.offset)*(1-ts.alpha) + float64(newOffset)*ts.alpha)
	}
	ts.latency = rtt
	ts.lastSync = time.Now()
	ts.healthy = rtt < 100 // healthy if RTT < 100ms
	offset := ts.offset
	latency := ts.latency
	healthy := ts.healthy
	ts.mu.Unlock()

	// Signal readiness after first successful sync
	ts.readyOnce.Do(func() {
		close(ts.ready)
		ts.logger.Info("🟢 TimeSync ready")
	})

	if healthy {
		ts.logger.Info("🟢 Time sync OK",
			"offset_ms", offset,
			"latency_ms", latency,
		)
	} else {
		ts.logger.Warn("🟡 Time sync high latency",
			"offset_ms", offset,
			"latency_ms", latency,
		)
	}
}

// GetServerTime returns the estimated current server time in milliseconds.
func (ts *TimeSync) GetServerTime() int64 {
	ts.mu.RLock()
	offset := ts.offset
	ts.mu.RUnlock()
	return time.Now().UnixMilli() + offset
}

// Offset returns the current clock offset in milliseconds.
func (ts *TimeSync) Offset() int64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.offset
}

// LatencyMs returns the last measured round-trip time in milliseconds.
func (ts *TimeSync) LatencyMs() int64 {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.latency
}

// IsHealthy returns true if the time sync is in a good state.
func (ts *TimeSync) IsHealthy() bool {
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	maxAge := ts.interval * 3
	if maxAge < 30*time.Second {
		maxAge = 30 * time.Second
	}
	return ts.healthy && time.Since(ts.lastSync) < maxAge
}

// MsUntilTarget returns the milliseconds until a target server timestamp.
func (ts *TimeSync) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - ts.GetServerTime()
}

// Now returns the estimated current server time as a time.Time value.
// This applies the EMA-smoothed offset to the local clock.
func (ts *TimeSync) Now() time.Time {
	return time.UnixMilli(ts.GetServerTime())
}

// Until returns the duration from server-now until the target time.
// Equivalent to time.Until(target) but uses the synced server clock.
func (ts *TimeSync) Until(target time.Time) time.Duration {
	return time.Duration(target.UnixMilli()-ts.GetServerTime()) * time.Millisecond
}
