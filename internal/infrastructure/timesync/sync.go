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
func New(client exchange.Client, log *slog.Logger, interval time.Duration) *TimeSync {
	return &TimeSync{
		client:   client,
		alpha:    0.3,
		interval: interval,
		logger:   log.With("component", "timesync"),
		ready:    make(chan struct{}),
	}
}

// WaitReady blocks until the first successful time sync completes or context is cancelled.
func (ts *TimeSync) WaitReady(ctx context.Context) error {
	select {
	case <-ts.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Start begins the continuous time synchronization loop.
// Exits when ctx is cancelled.
func (ts *TimeSync) Start(ctx context.Context) {
	ts.logger.InfoContext(ctx, "⏱️  Starting time sync service...", slog.Duration("interval", ts.interval))
	defer ts.logger.InfoContext(ctx, "⏱️  Time sync stopped")

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
		ts.logger.ErrorContext(ctx, "🔴 Time sync failed", slog.Any("error", err))
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
		ts.logger.InfoContext(ctx, "🟢 TimeSync ready")
	})

	if healthy {
		ts.logger.InfoContext(ctx, "🟢 Time sync OK",
			slog.Int64("offset_ms", offset),
			slog.Int64("latency_ms", latency),
		)
	} else {
		ts.logger.WarnContext(ctx, "🟡 Time sync high latency",
			slog.Int64("offset_ms", offset),
			slog.Int64("latency_ms", latency),
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
	maxAge := max(ts.interval*3, 30*time.Second)
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

// Sleep blocks until the duration elapses or the context is cancelled.
// This wraps time.After to allow tests to mock out time delays.
func (ts *TimeSync) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
