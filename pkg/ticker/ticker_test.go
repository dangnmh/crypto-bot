package ticker_test

import (
	"context"
	"sync/atomic"
	"testing"

	"crypto-bot/pkg/ticker"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRun_FiresAndStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int32

	done := make(chan struct{})
	go func() {
		ticker.Run(ctx, 50*time.Millisecond, func() bool {
			count.Add(1)
			return true
		})
		close(done)
	}()

	// Wait for at least 2 ticks.
	time.Sleep(150 * time.Millisecond)
	cancel()
	<-done

	assert.GreaterOrEqual(t, int(count.Load()), 2, "expected at least 2 ticks")
}

func TestRun_StopsOnFalseReturn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var count atomic.Int32

	done := make(chan struct{})
	go func() {
		ticker.Run(ctx, 50*time.Millisecond, func() bool {
			count.Add(1)
			return count.Load() < 3
		})
		close(done)
	}()

	select {
	case <-done:
		// Expected — task returned false.
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout — ticker.Run should stop when task returns false")
	}

	assert.Equal(t, int32(3), count.Load(), "expected exactly 3 ticks")
}

func TestRunImmediate_FiresImmediately(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var firstCallTime time.Time

	done := make(chan struct{})
	start := time.Now()
	go func() {
		ticker.RunImmediate(ctx, 5*time.Second, func() bool {
			firstCallTime = time.Now()
			cancel() // Stop after first call.
			return true
		})
		close(done)
	}()

	<-done

	elapsed := firstCallTime.Sub(start)
	assert.Less(t, elapsed, 100*time.Millisecond, "first call should happen immediately")
}

func TestRunImmediate_StopsIfFirstCallReturnsFalse(t *testing.T) {
	t.Parallel()

	var count atomic.Int32

	done := make(chan struct{})
	go func() {
		ticker.RunImmediate(context.Background(), 50*time.Millisecond, func() bool {
			count.Add(1)
			return false // Stop immediately.
		})
		close(done)
	}()

	select {
	case <-done:
		// Expected.
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout — ticker.RunImmediate should stop after first call returns false")
	}

	assert.Equal(t, int32(1), count.Load(), "expected 1 call")
}

func TestRandomJitterDuration(t *testing.T) {
	t.Parallel()

	t.Run("zero or negative jitter returns base interval", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, 10*time.Second, ticker.RandomJitterDuration(10*time.Second, 0))
		assert.Equal(t, 10*time.Second, ticker.RandomJitterDuration(10*time.Second, -5*time.Second))
	})

	t.Run("zero or negative base clamped to min duration", func(t *testing.T) {
		t.Parallel()
		assert.Equal(t, time.Millisecond, ticker.RandomJitterDuration(0, 0))
		assert.Equal(t, time.Millisecond, ticker.RandomJitterDuration(-1*time.Second, 0))
	})

	t.Run("jitter within bounds", func(t *testing.T) {
		t.Parallel()
		base := 10 * time.Second
		jitter := 2 * time.Second
		for range 100 {
			dur := ticker.RandomJitterDuration(base, jitter)
			assert.GreaterOrEqual(t, dur, 8*time.Second)
			assert.LessOrEqual(t, dur, 12*time.Second)
		}
	})

	t.Run("clamping when jitter >= base", func(t *testing.T) {
		t.Parallel()
		base := 50 * time.Millisecond
		jitter := 100 * time.Millisecond
		for range 50 {
			dur := ticker.RandomJitterDuration(base, jitter)
			assert.GreaterOrEqual(t, dur, time.Millisecond)
			assert.LessOrEqual(t, dur, 150*time.Millisecond)
		}
	})
}

func TestRunWithJitter_FiresAndStopsOnCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	var count atomic.Int32

	done := make(chan struct{})
	go func() {
		ticker.RunWithJitter(ctx, 30*time.Millisecond, 10*time.Millisecond, func() bool {
			count.Add(1)
			return true
		})
		close(done)
	}()

	time.Sleep(120 * time.Millisecond)
	cancel()
	<-done

	assert.GreaterOrEqual(t, int(count.Load()), 2, "expected at least 2 ticks with jitter")
}

func TestRunWithJitter_StopsOnFalseReturn(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var count atomic.Int32

	done := make(chan struct{})
	go func() {
		ticker.RunWithJitter(ctx, 20*time.Millisecond, 5*time.Millisecond, func() bool {
			count.Add(1)
			return count.Load() < 3
		})
		close(done)
	}()

	select {
	case <-done:
		// Expected
	case <-time.After(2 * time.Second):
		assert.Fail(t, "timeout — ticker.RunWithJitter should stop when task returns false")
	}

	assert.Equal(t, int32(3), count.Load(), "expected exactly 3 ticks")
}
