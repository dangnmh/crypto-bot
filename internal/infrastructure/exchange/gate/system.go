package gate

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

// Private raw methods.

func (c *Client) rawGetServerTime(ctx context.Context) (*gateSystemTime, error) {
	body, err := c.RawRequest(ctx, "GET", "/spot/time", nil, nil)
	if err != nil {
		return nil, err
	}
	var result gateSystemTime
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return &result, nil
}

// Public mapper methods.

// WarmUp maintaining connection pool via periodic ping requests (/spot/time).
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up Gate.io connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		var result gateSystemTime
		err := c.sendRequest(ctx, http.MethodGet, "/spot/time", nil, nil, &result)
		if err != nil {
			c.logger.DebugContext(ctx, "Gate.io warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// Latency measures round-trip time of fetching server time (ms).
func (c *Client) Latency(ctx context.Context) (int64, error) {
	start := time.Now()
	var result gateSystemTime
	err := c.sendRequest(ctx, http.MethodGet, "/spot/time", nil, nil, &result)
	if err != nil {
		return 0, err
	}
	return time.Since(start).Milliseconds(), nil
}

// SupportLeverageOnOrder returns false since Gate.io doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// GetServerTime returns the Gate.io server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, fmt.Errorf("gate.io get server time: %w", err)
	}
	return resp.ServerTime, nil
}
