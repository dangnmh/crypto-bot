package pionex

import (
	"context"
	"time"

	"crypto-bot/pkg/xjson"
)

type pionexTradeTimeResponse struct {
	Result    bool         `json:"result"`
	Timestamp xjson.Number `json:"timestamp"`
}

func (c *Client) GetServerTime(ctx context.Context) (int64, error) {
	query := map[string]string{
		symbolKey: "BTC_USDT",
		limitKey:  "1",
	}
	body, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/trades", query)
	if err != nil {
		return 0, err
	}
	var resp pionexTradeTimeResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	return xjson.ToInt64(resp.Timestamp), nil
}

func (c *Client) WarmUp(ctx context.Context, interval time.Duration) {
	_, _ = c.GetServerTime(ctx)
}

func (c *Client) SupportLeverageOnOrder() bool {
	return false
}
