package ratelimit_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/pkg/ratelimit"

	"github.com/stretchr/testify/assert"
	"golang.org/x/time/rate"
)

func TestExchangeRateLimiter_Acquire_SkipRateLimit(t *testing.T) {
	t.Parallel()
	// Create a limiter with 0 limit and 0 burst so any normal Acquire would block or fail
	limiter := ratelimit.NewExchangeRateLimiter(rate.Limit(0), 0, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Normal acquire should block and fail with deadline exceeded
	err := limiter.Acquire(ctx, "/v5/order/create")
	assert.Error(t, err)

	// Acquire with WithSkipRateLimit should immediately succeed
	fastCtx := ratelimit.WithSkipRateLimit(context.Background())
	err = limiter.Acquire(fastCtx, "/v5/order/create")
	assert.NoError(t, err)
}
