package hotcoin

import (
	"context"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type serverTimeResponse struct {
	Timestamp int64 `json:"timestamp"`
}

// SupportLeverageOnOrder returns false since Hotcoin does not support setting leverage on orders directly.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp maintains active HTTP connection pools via periodic public checks.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetServerTime(ctx)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time call failed", "error", err)
		}
		return true
	})
}

// GetServerTime queries the current server millisecond timestamp.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/public/time", nil, nil, false)
	if err != nil {
		return 0, err
	}
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	return resp.Timestamp, nil
}
