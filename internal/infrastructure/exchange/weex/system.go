package weex

import (
	"context"
	"net/http"
	"time"

	"crypto-bot/pkg/ticker"
	"crypto-bot/pkg/xjson"
)

type serverTimeResponse struct {
	ServerTime int64 `json:"serverTime"`
}

// SupportLeverageOnOrder returns false since WEEX does not support setting leverage on orders directly.
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
	body, err := c.request(ctx, http.MethodGet, "/capi/v3/market/time", nil, nil, false)
	if err != nil {
		return 0, err
	}
	var wrapped struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data string `json:"data"`
	}
	if err := xjson.Unmarshal(body, &wrapped); err == nil && wrapped.Code == "00000" {
		var st int64
		if err := xjson.Unmarshal([]byte(wrapped.Data), &st); err == nil {
			return st, nil
		}
		// Data might be a JSON object inside wrapped response:
		var stObj serverTimeResponse
		if err := xjson.Unmarshal([]byte(wrapped.Data), &stObj); err == nil {
			return stObj.ServerTime, nil
		}
	}
	// Fallback to parsing directly:
	var resp serverTimeResponse
	if err := xjson.Unmarshal(body, &resp); err == nil && resp.ServerTime > 0 {
		return resp.ServerTime, nil
	}
	return 0, nil
}
