package binance

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
)

type binanceServerTimeRequest struct{}

// WarmUp maintains the connection pool via periodic pings.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		err := c.request(ctx, http.MethodGet, "/fapi/v1/ping", nil, false, nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Binance warmup connectivity check failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since Binance doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

func (c *Client) rawGetServerTime(ctx context.Context, _ binanceServerTimeRequest) (*checkServerTimeResponse, error) {
	var resp checkServerTimeResponse
	err := c.request(ctx, http.MethodGet, "/fapi/v1/time", nil, false, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

// GetServerTime returns the Binance server timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.rawGetServerTime(ctx, binanceServerTimeRequest{})
	if err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}
