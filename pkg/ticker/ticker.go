package ticker

import (
	"context"
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
