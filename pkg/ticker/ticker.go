package ticker

import (
	"context"
	"math/rand/v2"
	"time"
)

// Task is a function that executes periodically.
// Return false to stop the ticker loop early.
type Task func() bool

// Run starts a background loop that executes the task every interval.
// It waits for the given interval before the first execution.
// The loop stops when the context is cancelled or the task returns false.
func Run(ctx context.Context, interval time.Duration, task Task) {
	t := time.NewTicker(interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if !task() {
				return
			}
		}
	}
}

// RunImmediate executes the task immediately before starting the periodic loop.
func RunImmediate(ctx context.Context, interval time.Duration, task Task) {
	if !task() {
		return
	}
	Run(ctx, interval, task)
}

// RandomJitterDuration computes a randomized duration uniformly distributed in [base - jitter, base + jitter].
// If base <= 0, returns a minimum safe duration of time.Millisecond.
// If jitter <= 0, returns base.
// If base - jitter <= 0, the lower bound is clamped to time.Millisecond.
func RandomJitterDuration(base, jitter time.Duration) time.Duration {
	if base <= 0 {
		return time.Millisecond
	}
	if jitter <= 0 {
		return base
	}

	minDur := max(base-jitter, time.Millisecond)
	maxDur := max(base+jitter, minDur)

	delta := int64(maxDur - minDur)
	if delta <= 0 {
		return minDur
	}

	offset := rand.Int64N(delta + 1)
	return minDur + time.Duration(offset)
}

// RunWithJitter starts a background loop that executes task at dynamic intervals with jitter.
// The loop stops when the context is cancelled or the task returns false.
func RunWithJitter(ctx context.Context, baseInterval, jitter time.Duration, task Task) {
	for {
		dur := RandomJitterDuration(baseInterval, jitter)
		timer := time.NewTimer(dur)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
			if !task() {
				return
			}
		}
	}
}
