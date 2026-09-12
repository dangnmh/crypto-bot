package timesync

import (
	"context"
	"log/slog"
	"runtime"
	"slices"
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
// Uses Exponential Moving Average to smooth clock offset and maintains rolling minimum and median RTT for HTTP and WS.
type TimeSync struct {
	client exchange.Client
	mu     sync.RWMutex
	offset int64 // server - local (ms)

	// HTTP metrics
	httpLatency   int64 // last measured HTTP round-trip time (ms)
	httpMinRTT    int64 // rolling minimum HTTP round-trip time (ms)
	httpMedianRTT int64 // rolling median HTTP round-trip time (ms)
	httpSamples   []rttSample

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
	ts.httpLatency = rttMs
	ts.lastSync = now
	ts.healthy = rttMs < 100 // healthy if RTT < 100ms

	ts.updateHTTPSamples(rttMs, now)

	offset := ts.offset
	httpLatency := ts.httpLatency
	healthy := ts.healthy
	httpMinLatency := ts.httpMinRTT
	httpMedianLatency := ts.httpMedianRTT
	ts.mu.Unlock()

	// Signal readiness after first successful sync
	ts.readyOnce.Do(func() {
		close(ts.ready)
		ts.logger.InfoContext(ctx, "🟢 TimeSync ready")
	})

	ts.logSyncStatus(ctx, offset, httpLatency, httpMinLatency, httpMedianLatency, healthy)
}

func (ts *TimeSync) updateHTTPSamples(rttMs int64, now time.Time) {
	// Maintain rolling window of HTTP RTT samples (60s retention)
	ts.httpSamples = append(ts.httpSamples, rttSample{rtt: rttMs, at: now})
	cutoff := now.Add(-60 * time.Second)
	idx := 0
	for idx < len(ts.httpSamples) && ts.httpSamples[idx].at.Before(cutoff) {
		idx++
	}
	if idx > 0 {
		ts.httpSamples = ts.httpSamples[idx:]
	}

	// Calculate rolling minimum RTT across active HTTP samples
	minRTT := rttMs
	for _, s := range ts.httpSamples {
		if s.rtt < minRTT {
			minRTT = s.rtt
		}
	}
	ts.httpMinRTT = minRTT

	// Calculate rolling median RTT across active HTTP samples
	rtts := make([]int64, len(ts.httpSamples))
	for i, s := range ts.httpSamples {
		rtts[i] = s.rtt
	}
	slices.Sort(rtts)
	n := len(rtts)
	var medianRTT int64
	if n%2 == 1 {
		medianRTT = rtts[n/2]
	} else if n > 0 {
		medianRTT = (rtts[n/2-1] + rtts[n/2]) / 2
	}
	ts.httpMedianRTT = medianRTT
}

func (ts *TimeSync) logSyncStatus(ctx context.Context, offset, httpLatency, httpMinLatency, httpMedianLatency int64, healthy bool) {
	mode := exchange.TradeModeHTTP
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		mode = tmc.TradeMode()
	}
	wsLast := ts.WSLastLatencyMs()
	wsMin := ts.WSMinRTTMs()
	wsMedian := ts.WSMedianRTTMs()

	attrs := []any{
		slog.Int64("offset_ms", offset),
		slog.String("trade_mode", string(mode)),
		slog.Int64("http_latency_ms", httpLatency),
		slog.Int64("http_min_rtt_ms", httpMinLatency),
		slog.Int64("http_median_rtt_ms", httpMedianLatency),
	}
	if wsLast >= 0 {
		attrs = append(attrs, slog.Int64("ws_latency_ms", wsLast))
	}
	if wsMin >= 0 {
		attrs = append(attrs, slog.Int64("ws_min_rtt_ms", wsMin))
	}
	if wsMedian >= 0 {
		attrs = append(attrs, slog.Int64("ws_median_rtt_ms", wsMedian))
	}

	if healthy {
		ts.logger.InfoContext(ctx, "🟢 Time sync OK", attrs...)
	} else {
		ts.logger.WarnContext(ctx, "🟡 Time sync high latency", attrs...)
	}
}

// SyncNow forces an immediate time synchronization round synchronously.
// If running in WebSocket trade mode, it triggers a WebSocket ping and HTTP sync concurrently,
// waiting for both to complete to ensure both clock offset and WebSocket RTT are fresh.
func (ts *TimeSync) SyncNow(ctx context.Context) {
	var wg sync.WaitGroup
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok && tmc.TradeMode() == exchange.TradeModeWS {
		if wsExec := tmc.WSTradeExecutor(); wsExec != nil && wsExec.IsReady() {
			if pinger, ok := wsExec.(interface{ Ping(context.Context) error }); ok {
				wg.Go(func() {
					_ = pinger.Ping(ctx)
				})
			}
		}
	}
	ts.syncOnce(ctx)
	wg.Wait()
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

// LatencyMs returns the rolling median round-trip time in milliseconds across recent samples
// for the currently active trade mode of the exchange client (WS if in WS trade mode, HTTP otherwise).
func (ts *TimeSync) LatencyMs() int64 {
	if ts == nil {
		return 0
	}
	mode := exchange.TradeModeHTTP
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		mode = tmc.TradeMode()
	}
	return ts.LatencyForMode(mode)
}

// LatencyForMode returns the rolling median round-trip time in milliseconds for the specified trade mode.
// If mode is TradeModeWS, it returns the WebSocket median RTT (falling back to HTTP if WS is not ready or has no samples).
func (ts *TimeSync) LatencyForMode(mode exchange.TradeMode) int64 {
	if ts == nil {
		return 0
	}
	switch mode {
	case exchange.TradeModeWS:
		if wsLat := ts.WSLatencyMs(); wsLat >= 0 {
			return wsLat
		}
		return ts.HTTPLatencyMs()
	case exchange.TradeModeHTTP:
		return ts.HTTPLatencyMs()
	default:
		return ts.HTTPLatencyMs()
	}
}

// HTTPLatencyMs returns the rolling median round-trip time in milliseconds for HTTP requests,
// falling back to rolling minimum or last measured latency if fewer samples exist.
func (ts *TimeSync) HTTPLatencyMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	if ts.httpMedianRTT > 0 {
		return ts.httpMedianRTT
	}
	if ts.httpMinRTT > 0 {
		return ts.httpMinRTT
	}
	return ts.httpLatency
}

// HTTPMinRTTMs returns the rolling minimum round-trip time in milliseconds for HTTP requests.
func (ts *TimeSync) HTTPMinRTTMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.httpMinRTT
}

// HTTPMedianRTTMs returns the rolling median round-trip time in milliseconds for HTTP requests.
func (ts *TimeSync) HTTPMedianRTTMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.httpMedianRTT
}

// MinRTTMs returns the rolling minimum RTT in milliseconds for the currently active trade mode.
func (ts *TimeSync) MinRTTMs() int64 {
	if ts == nil {
		return 0
	}
	mode := exchange.TradeModeHTTP
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		mode = tmc.TradeMode()
	}
	if mode == exchange.TradeModeWS {
		if wsMin := ts.WSMinRTTMs(); wsMin >= 0 {
			return wsMin
		}
	}
	return ts.HTTPMinRTTMs()
}

// MedianRTTMs returns the rolling median RTT in milliseconds for the currently active trade mode.
func (ts *TimeSync) MedianRTTMs() int64 {
	return ts.LatencyMs()
}

// LastHTTPLatencyMs returns the raw single-ping round-trip time of the most recent HTTP sync.
func (ts *TimeSync) LastHTTPLatencyMs() int64 {
	if ts == nil {
		return 0
	}
	ts.mu.RLock()
	defer ts.mu.RUnlock()
	return ts.httpLatency
}

// WSMinRTTMs returns the rolling minimum WebSocket round-trip time in milliseconds,
// or -1 if the exchange client is not in WS mode, WS executor is not ready, or no pong was received.
func (ts *TimeSync) WSMinRTTMs() int64 {
	if ts == nil {
		return -1
	}
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		if wsExec := tmc.WSTradeExecutor(); wsExec != nil && wsExec.IsReady() {
			if minProv, ok := wsExec.(interface{ MinLatencyMs() int64 }); ok {
				return minProv.MinLatencyMs()
			}
			return wsExec.LatencyMs()
		}
	}
	return -1
}

// WSLatencyMs returns the rolling median or last measured WebSocket round-trip time in milliseconds,
// or -1 if the exchange client is not in WS mode, WS executor is not ready, or no pong was received.
func (ts *TimeSync) WSLatencyMs() int64 {
	if ts == nil {
		return -1
	}
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		if wsExec := tmc.WSTradeExecutor(); wsExec != nil && wsExec.IsReady() {
			return wsExec.LatencyMs()
		}
	}
	return -1
}

// WSMedianRTTMs returns the rolling median WebSocket round-trip time in milliseconds,
// or -1 if the exchange client is not in WS mode, WS executor is not ready, or no pong was received.
func (ts *TimeSync) WSMedianRTTMs() int64 {
	return ts.WSLatencyMs()
}

// WSLastLatencyMs returns the raw single-ping round-trip time of the most recent WebSocket pong,
// or -1 if the exchange client is not in WS mode, WS executor is not ready, or no pong was received.
func (ts *TimeSync) WSLastLatencyMs() int64 {
	if ts == nil {
		return -1
	}
	if tmc, ok := ts.client.(exchange.TradeModeConfigurable); ok {
		if wsExec := tmc.WSTradeExecutor(); wsExec != nil && wsExec.IsReady() {
			if lastProv, ok := wsExec.(interface{ LastLatencyMs() int64 }); ok {
				return lastProv.LastLatencyMs()
			}
			return wsExec.LatencyMs()
		}
	}
	return -1
}

// LastLatencyMs returns the raw single-ping round-trip time of the most recent HTTP sync.
func (ts *TimeSync) LastLatencyMs() int64 {
	return ts.LastHTTPLatencyMs()
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
