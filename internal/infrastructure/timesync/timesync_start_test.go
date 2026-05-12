package timesync_test

import (
	"context"
	"testing"

	"crypto-bot/internal/infrastructure/timesync"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestTimeSync_Start(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, 10*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// Start runs RunImmediate which will sync once then block on interval.
	go ts.Start(ctx)

	// WaitReady should return after the first sync.
	ts.WaitReady(ctx)

	assert.True(t, ts.IsHealthy(), "should be healthy after first sync")
	assert.NotZero(t, ts.GetServerTime())
}

func TestTimeSync_Start_ReturnsOnCancel(t *testing.T) {
	t.Parallel()

	mc := &mockClient{serverTime: time.Now().UnixMilli()}
	ts := timesync.New(mc, time.Hour) // very long interval

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		ts.Start(ctx)
		close(done)
	}()

	// Let it sync once.
	ts.WaitReady(ctx)
	cancel()

	// Start should return after ctx cancelled.
	select {
	case <-done:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}
