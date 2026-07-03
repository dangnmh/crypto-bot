package deepcoin

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type deepcoinServerTimeResponse struct {
	Ts xjson.Number `json:"ts"`
}

func (c *Client) rawGetServerTime(ctx context.Context) (*deepcoinServerTimeResponse, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/time", nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinServerTimeResponse](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up Deepcoin connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.rawGetServerTime(ctx)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time call failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since Deepcoin doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// GetServerTime returns the current server time in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseInt(string(res.Ts), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return val, nil
}
