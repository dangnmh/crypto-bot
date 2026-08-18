package domain

import (
	"context"
	"time"
)

var _ Clock = SystemClock{}

// SystemClock provides default system time implementing domain.Clock.
type SystemClock struct{}

func (SystemClock) Now() time.Time                  { return time.Now() }
func (SystemClock) GetServerTime() int64            { return time.Now().UnixMilli() }
func (SystemClock) Until(t time.Time) time.Duration { return time.Until(t) }
func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(d):
		return nil
	}
}
func (SystemClock) LatencyMs() int64                 { return 0 }
func (SystemClock) Offset() int64                    { return 0 }
func (SystemClock) IsHealthy() bool                  { return true }
func (SystemClock) MsUntilTarget(target int64) int64 { return target - time.Now().UnixMilli() }
