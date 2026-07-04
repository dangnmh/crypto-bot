package ratelimit

import (
	"context"
	"strings"
	"sync"

	"golang.org/x/time/rate"
)

// EndpointConfig defines the limit and weight for a specific API path pattern.
type EndpointConfig struct {
	Limit  rate.Limit // Requests per second allowed for this path prefix
	Burst  int        // Max burst size
	Weight int        // The weight of a single request to this path prefix
}

// ExchangeRateLimiter manages multi-tier (global + path) and weight-based rate limiting.
type ExchangeRateLimiter struct {
	globalLimiter *rate.Limiter
	globalWeight  int
	pathConfigs   map[string]EndpointConfig
	pathLimiters  map[string]*rate.Limiter
	mu            sync.RWMutex
}

// NewExchangeRateLimiter creates a new rate limiter manager.
func NewExchangeRateLimiter(globalLimit rate.Limit, globalBurst int, configs map[string]EndpointConfig) *ExchangeRateLimiter {
	return &ExchangeRateLimiter{
		globalLimiter: rate.NewLimiter(globalLimit, globalBurst),
		globalWeight:  1,
		pathConfigs:   configs,
		pathLimiters:  make(map[string]*rate.Limiter),
	}
}

// Acquire blocks until the request is allowed under all registered rate limits.
func (rl *ExchangeRateLimiter) Acquire(ctx context.Context, path string) error {
	prefix, config, hasConfig := rl.resolveConfig(path)

	// 1. Consume tokens from path-specific limiter if configured
	if hasConfig && config.Limit > 0 {
		limiter := rl.getPathLimiter(prefix, config)
		if err := limiter.Wait(ctx); err != nil {
			return err
		}
	}

	// 2. Consume N tokens globally matching request weight
	weight := rl.globalWeight
	if hasConfig && config.Weight > 0 {
		weight = config.Weight
	}

	return rl.globalLimiter.WaitN(ctx, weight)
}

func (rl *ExchangeRateLimiter) resolveConfig(path string) (string, EndpointConfig, bool) {
	for prefix, cfg := range rl.pathConfigs {
		if strings.HasPrefix(path, prefix) {
			return prefix, cfg, true
		}
	}
	return "", EndpointConfig{}, false
}

func (rl *ExchangeRateLimiter) getPathLimiter(prefix string, cfg EndpointConfig) *rate.Limiter {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	limiter, exists := rl.pathLimiters[prefix]
	if !exists {
		limiter = rate.NewLimiter(cfg.Limit, cfg.Burst)
		rl.pathLimiters[prefix] = limiter
	}
	return limiter
}
