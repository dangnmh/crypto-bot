package bybit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type bybitServerTimeRequest struct{}

// WarmUp maintains the connection pool.
func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	ticker.RunImmediate(ctx, interval, func() bool {
		_, err := c.GetServerTime(ctx)
		if err != nil {
			c.logger.Debug("Bybit warmup ping failed", slog.Any("error", err))
		}
		return true
	})
}

// SupportLeverageOnOrder returns true since Bybit V5 supports set-leverage inside create order request.
func (c *Client) SupportLeverageOnOrder() bool {
	return true
}

func (c *Client) rawGetServerTime(ctx context.Context, _ bybitServerTimeRequest) (int64, error) {
	body, err := c.RawRequest(ctx, http.MethodGet, "/v5/market/time", nil, nil)
	if err != nil {
		return 0, fmt.Errorf("bybit get server time: %w", err)
	}
	var resp struct {
		RetCode int    `json:"retCode"`
		RetMsg  string `json:"retMsg"`
		Time    int64  `json:"time"`
	}
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, fmt.Errorf("bybit get server time json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		return 0, fmt.Errorf("bybit get server time error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return resp.Time, nil
}

// GetServerTime returns the Bybit server timestamp in milliseconds.
func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	return c.rawGetServerTime(ctx, bybitServerTimeRequest{})
}
