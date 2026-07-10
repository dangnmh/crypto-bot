package bitunix

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitunixKlineItem struct {
	Open     xjson.Number `json:"open"`
	High     xjson.Number `json:"high"`
	Low      xjson.Number `json:"low"`
	Close    xjson.Number `json:"close"`
	QuoteVol xjson.Number `json:"quoteVol"`
	BaseVol  xjson.Number `json:"baseVol"`
	Time     int64        `json:"time"`
}

type bitunixKlineResponse struct {
	Code int                `json:"code"`
	Data []bitunixKlineItem `json:"data"`
	Msg  string             `json:"msg"`
}

// FetchKlines fetches public K-lines for bitunix.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(strings.ReplaceAll(symbol, "_", ""))
	params := map[string]string{
		"symbol":   cleanSymbol,
		"interval": string(interval),
	}

	if !start.IsZero() {
		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/market/kline", params, nil)
	if err != nil {
		return nil, fmt.Errorf("bitunix request klines: %w", err)
	}

	var resp bitunixKlineResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bitunix unmarshal klines: %w", err)
	}

	if resp.Code != 200 {
		return nil, fmt.Errorf("bitunix API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		klines = append(klines, exchange.Kline{
			Timestamp: item.Time,
			Open:      xjson.ToFloat64(item.Open),
			High:      xjson.ToFloat64(item.High),
			Low:       xjson.ToFloat64(item.Low),
			Close:     xjson.ToFloat64(item.Close),
			Volume:    xjson.ToFloat64(item.QuoteVol),
			Amount:    xjson.ToFloat64(item.BaseVol),
		})
	}

	return klines, nil
}
