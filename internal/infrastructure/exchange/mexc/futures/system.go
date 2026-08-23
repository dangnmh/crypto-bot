package futures

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/ticker"
)

// WarmUp pre-establishes connection pool and maintains it via periodic ping requests.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.base.Logger().InfoContext(ctx, "🔗 Warming up connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.base.Request(ctx, http.MethodGet, "/api/v1/contract/ping", nil, nil, false)
		if err != nil {
			c.base.Logger().DebugContext(ctx, "Warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns true since MEXC supports leverage parameter.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) rawPing(ctx context.Context) ([]byte, error) {
	return c.base.Request(ctx, http.MethodGet, "/api/v1/contract/ping", nil, nil, false)
}

func (c *Client) rawGetServerTime(ctx context.Context) (int64, error) {
	body, err := c.rawPing(ctx)
	if err != nil {
		return 0, err
	}
	resp, err := mexc.ParseFuturesResponse[int64](body)
	if err != nil {
		return 0, err
	}
	return resp.Data, nil
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
