package futures

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.base.Logger().InfoContext(ctx, "🔗 Warming up KuCoin Futures connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.base.Request(ctx, http.MethodGet, "/api/v1/timestamp", nil, nil, false)
		if err != nil {
			c.base.Logger().DebugContext(ctx, "Warmup server time call failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns true since KuCoin Futures supports leverage on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

// GetServerTime returns the KuCoin server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/timestamp", nil, nil, false)
	if err != nil {
		return 0, err
	}

	var numVal int64
	if err := xjson.Unmarshal(body, &numVal); err == nil {
		return numVal, nil
	}

	data, err := kucoin.ParseResponse[int64](body, "server_time")
	if err != nil {
		return 0, err
	}

	return data, nil
}

// Ping sends a lightweight ping to verify connection.
func (c *Client) Ping(ctx context.Context) error {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/timestamp", nil, nil, false)
	if err != nil {
		return err
	}
	var raw json.RawMessage
	return xjson.Unmarshal(body, &raw)
}
