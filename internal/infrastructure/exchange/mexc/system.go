package mexc

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
)

// WarmUp pre-establishes connection pool and maintains it via periodic ping requests.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, "/api/v1/contract/ping", nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since MEXC doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) rawPing(ctx context.Context) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/api/v1/contract/ping", nil, nil)
}

func (c *Client) rawGetServerTime(ctx context.Context) (int64, error) {
	body, err := c.rawPing(ctx)
	if err != nil {
		return 0, err
	}
	return ParseResponse[int64](body, "server_time")
}

// Ping checks connectivity to the MEXC API server.
func (c *Client) Ping(ctx context.Context) error {
	_, err := c.rawPing(ctx)
	return err
}

// GetServerTime returns the MEXC server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.rawGetServerTime(ctx)
}
