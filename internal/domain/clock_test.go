package domain_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSystemClock(t *testing.T) {
	t.Parallel()

	clk := domain.SystemClock{}

	assert.WithinDuration(t, time.Now(), clk.Now(), 50*time.Millisecond)
	assert.InDelta(t, time.Now().UnixMilli(), clk.GetServerTime(), 50)
	assert.True(t, clk.IsHealthy())
	assert.Equal(t, int64(0), clk.LatencyMs())
	assert.Equal(t, int64(0), clk.Offset())

	future := time.Now().Add(500 * time.Millisecond)
	assert.InDelta(t, 500*time.Millisecond, clk.Until(future), float64(50*time.Millisecond))
	assert.InDelta(t, 500, clk.MsUntilTarget(time.Now().UnixMilli()+500), 50)

	// Sleep success
	ctx := context.Background()
	start := time.Now()
	err := clk.Sleep(ctx, 20*time.Millisecond)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, time.Since(start), 15*time.Millisecond)

	// Sleep cancelled
	cancelCtx, cancel := context.WithCancel(context.Background())
	cancel()
	err = clk.Sleep(cancelCtx, 100*time.Millisecond)
	require.ErrorIs(t, err, context.Canceled)
}
