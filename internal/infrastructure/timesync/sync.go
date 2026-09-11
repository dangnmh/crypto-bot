package timesync

import (
	"context"
	"log/slog"
	"runtime"
	"sync"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/ticker"
)

type rttSample struct {
	rtt int64
	at  time.Time
}

// TimeSync continuously synchronizes local time with exchange server time.
// Uses Exponential Moving Average to smooth clock offset and maintains a rolling minimum RTT.
type TimeSync struct {
	client    exchange.Client
	mu        sync.RWMutex
	offset    int64 // server - local (ms)
	latency   int64 // last measured round-trip time (ms)
	minRTT    int64 // rolling minimum round-trip time (ms)
	samples   []rttSample
	lastSync  time.Time
	healthy   bool
	alpha     float64 // EMA smoothing factor
	interval  time.Duration
	logger    *slog.Logger
	ready     chan struct{}
	readyOnce sync.Once
	sleeper   func(ctx context.Context, d time.Duration) error
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
	localBefore := time.Now()
	serverTime, err := ts.client.GetServerTime(ctx)
	localAfter := time.Now()
	if err != nil {
		ts.mu.Lock()
		ts.healthy = false
		ts.mu.Unlock()
		ts.logger.ErrorContext(ctx, "🔴 Time sync failed", slog.Any("error", err))
		return
	}

	rttDur := localAfter.Sub(localBefore)
	rttMs := rttDur.Milliseconds()
	localMid := localBefore.Add(rttDur / 2)
	newOffset := serverTime - localMid.UnixMilli()

	now := time.Now()
	ts.mu.Lock()
	if ts.lastSync.IsZero() {
		// First sync — use raw value
		ts.offset = newOffset
	} else {
		// EMA smoothing
		ts.offset = int64(float64(ts.offset)*(1-ts.alpha) + float64(newOffset)*ts.alpha)
	}
	ts.latency = rttMs
	ts.lastSync = now
	ts.healthy = rttMs < 100 // healthy if RTT < 100ms

	// Maintain rolling window of RTT samples (60s retention)
	ts.samples = append(ts.samples, rttSample{rtt: rttMs, at: now})
	cutoff := now.Add(-60 * time.Second)
	idx := 0
	for idx < len(ts.samples) && ts.samples[idx].at.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		ts.samples = ts.samples[idx:]
	}

	// Calculate rolling minimum RTT across active samples
	minRTT := rttMs
	for _, s := range ts.samples {
		if s.rtt < minRTT {
			minRTT = s.rtt
		}
	}
	ts.minRTT = minRTT

	offset := ts.offset
	latency := ts.latency
	healthy := ts.healthy
	minLatency := ts.minRTT
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
			slog.Int64("min_rtt_ms", minLatency),
		)
	} else {
		ts.logger.WarnContext(ctx, "🟡 Time sync high latency",
			slog.Int64("offset_ms", offset),
			slog.Int64("latency_ms", latency),
			slog.Int64("min_rtt_ms", minLatency),
		)
	}
}

// SyncNow forces an immediate time synchronization round synchronously.
func (ts *TimeSync) SyncNow(ctx context.Context) {
	ts.syncOnce(ctx)
}

// GetServerTime returns the estimated current server time in milliseconds.
func (ts *TimeSync) GetServerTime() int64 {
	if ts == nil {
		return time.Now().UnixMilli()
	}
	ts.mu.RLock()
	offset := ts.offset
	ts.mu.RUnlock()
	return time.Now().UnixMilli() + offset
}

// Offset returns the current clock offset in milliseconds.
func (ts *TimeSync) Offset() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.offset
}

// LatencyMs returns the rolling minimum round-trip time in milliseconds across recent samples.
func (ts *TimeSync) LatencyMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	if ts.minRTT > 0 {
		return ts.minRTT
	}
	return ts.latency
}

// LastLatencyMs returns the raw single-ping round-trip time of the most recent sync.
func (ts *TimeSync) LastLatencyMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.latency
}

// IsHealthy returns true if the time sync is in a good state.
func (ts *TimeSync) IsHealthy() bool {
	if ts == nil {
		return false
	}
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
	if ts == nil {
		return time.Until(target)
	}
	return time.Duration(target.UnixMilli()-ts.GetServerTime()) * time.Millisecond
}

// SetSleeper overrides the sleep implementation (useful for tests to avoid real time delays).
func (ts *TimeSync) SetSleeper(fn func(ctx context.Context, d time.Duration) error) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.sleeper = fn
}

// Sleep blocks until the duration elapses or the context is cancelled.
// This wraps time.After to allow tests to mock out time delays.
func (ts *TimeSync) Sleep(ctx context.Context, d time.Duration) error {
	if ts != nil {
		ts.mu.RLock()
		sleeper := ts.sleeper
		ts.mu.RUnlock()
		if sleeper != nil {
			return sleeper(ctx, d)
		}
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}

// PrecisionSleepUntil blocks until target arrives using a hybrid sleep + spin-wait strategy:
// 1. Normal sleep until leadTime (3ms) before target to release CPU.
// 2. High-resolution monotonic spin-wait with locked OS thread for the final remaining milliseconds.
func (ts *TimeSync) PrecisionSleepUntil(ctx context.Context, target time.Time) error {
	if sleeper := ts.getSleeper(); sleeper != nil {
		dur := ts.Until(target)
		if dur <= 0 {
			return nil
		}
		return sleeper(ctx, dur)
	}

	const leadTime = 3 * time.Millisecond
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := ts.currentTime()
		remaining := target.Sub(now)
		if remaining <= 0 {
			return nil
		}
		if remaining <= leadTime {
			return ts.spinWaitUntil(ctx, target)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(remaining - leadTime):
		}
	}
}

func (ts *TimeSync) getSleeper() func(context.Context, time.Duration) error {
	if ts == nil {
		return nil
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.sleeper
}

func (ts *TimeSync) currentTime() time.Time {
	if ts != nil {
		return ts.Now()
	}
	return time.Now()
}

func (ts *TimeSync) spinWaitUntil(ctx context.Context, target time.Time) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	var offset int64
	if ts != nil {
		offset = ts.Offset()
	}
	targetLocal := target.Add(-time.Duration(offset) * time.Millisecond)

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		now := time.Now()
		if now.After(targetLocal) || now.Equal(targetLocal) {
			return nil
		}
	}
}
