package aster

import (
	"context"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type asterServerTime struct {
	ServerTime int64 `json:"serverTime"`
}

func (c *Client) rawGetServerTime(ctx context.Context) (*asterServerTime, error) {
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/time", nil, false)
	if err != nil {
		return nil, err
	}
	var resp asterServerTime
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, err
	}
	return resp.ServerTime, nil
}

func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetServerTime(ctx)
		if err != nil {
			c.logger.DebugContext(ctx, "Warmup server time ping failed", "error", err)
		}
		return true
	})
}
