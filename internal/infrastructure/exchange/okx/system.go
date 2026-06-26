package okx

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/pkg/ticker"
)

type okxServerTimeResponse struct {
	Ts string `json:"ts"`
}

type okxServerTimeRequest struct{}

// WarmUp pre-establishes connection pool and maintains it via periodic public calls.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	c.logger.InfoContext(ctx, "🔗 Warming up OKX connection pool...", slog.Duration("interval", interval))

	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetCtx(ctx, pathServerTime, nil)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time call failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns false since OKX doesn't support setting leverage directly on orders.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

func (c *Client) rawGetServerTime(ctx context.Context, _ okxServerTimeRequest) (*okxServerTimeResponse, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, pathServerTime, nil, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxServerTimeResponse](body, "server_time")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// GetServerTime returns the OKX server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	res, err := c.rawGetServerTime(ctx, okxServerTimeRequest{})
	if err != nil {
		return 0, err
	}
	val, err := strconv.ParseInt(res.Ts, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse server time: %w", err)
	}
	return val, nil
}
