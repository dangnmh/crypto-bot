package bitmart

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"crypto-bot/pkg/xjson"
)

type serverTimeResponse struct {
	Code int `json:"code"`
	Data struct {
		ServerTime int64 `json:"server_time"`
	} `json:"data"`
}

// Private raw methods.

func (c *Client) rawGetServerTime(ctx context.Context) (*serverTimeResponse, error) {
	body, err := c.request(ctx, http.MethodGet, "/system/time", nil)
	if err != nil {
		return nil, err
	}
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal server time: %w", err)
	}
	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d", resp.Code)
	}
	return &resp, nil
}

// Public mapper methods.

// SupportLeverageOnOrder returns false for Bitmart since leverage must be configured on the account first.
func (c *Client) SupportLeverageOnOrder() bool {
	return false
}

// WarmUp pings the server to warm up HTTP connections.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	_, _ = c.GetServerTime(ctx)
}

// GetServerTime returns system time from system/time.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	resp, err := c.rawGetServerTime(ctx)
	if err != nil {
		return 0, err
	}
	return resp.Data.ServerTime, nil
}
