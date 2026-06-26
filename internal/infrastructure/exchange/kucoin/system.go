package kucoin

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type kucoinServerTimeRequest struct{}

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up KuCoin connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, pathServerTime, nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time call failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since KuCoin doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) rawGetServerTime(ctx context.Context, _ kucoinServerTimeRequest) (json.RawMessage, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathServerTime, nil, nil)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// GetServerTime returns the KuCoin server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.rawGetServerTime(ctx, kucoinServerTimeRequest{})
	if err != nil {
		return 0, err
	}

	var numVal int64
	if err := xjson.Unmarshal(body, &numVal); err == nil {
		return numVal, nil
	}

	data, err := ParseResponse[int64](body, "server_time")
	if err != nil {
		return 0, err
	}

	return data, nil
}
