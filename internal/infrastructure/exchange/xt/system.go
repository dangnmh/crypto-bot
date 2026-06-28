package xt

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/pkg/xjson"
)

type xtTimeResponse struct {
	ReturnCode int64  `json:"returnCode"`
	MsgInfo    string `json:"msgInfo"`
	Result     int64  `json:"result"`
}

func (c *Client) rawGetServerTime(ctx context.Context) (int64, error) {
	body, err := c.request(ctx, "GET", "/future/market/v1/public/time", nil, nil, false)
	if err != nil {
		return 0, err
	}
	var resp xtTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal time response: %w", err)
	}
	return resp.Result, nil
}

// GetServerTime returns current Unix server time from XT.com.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.rawGetServerTime(ctx)
}

// SupportLeverageOnOrder returns true if leverage can be passed directly with order placement payloads.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp pings the API to keep connections hot.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_, _ = c.GetServerTime(ctx)
		}
	}
}
