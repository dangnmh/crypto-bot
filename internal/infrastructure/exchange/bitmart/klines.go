package bitmart

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

var intervalMap = map[exchange.Interval]int{
	exchange.Interval1m:  1,
	exchange.Interval3m:  3,
	exchange.Interval5m:  5,
	exchange.Interval15m: 15,
	exchange.Interval30m: 30,
	exchange.Interval1h:  60,
	exchange.Interval2h:  120,
	exchange.Interval4h:  240,
	exchange.Interval6h:  240,
	exchange.Interval8h:  240,
	exchange.Interval12h: 240,
	exchange.Interval1d:  1440,
	exchange.Interval1w:  10080,
	exchange.Interval1M:  43200,
}

func mapBitmartInterval(interval exchange.Interval) int {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return 1
}

type bitmartKlineItem struct {
	LowPrice   xjson.Number `json:"low_price"`
	HighPrice  xjson.Number `json:"high_price"`
	OpenPrice  xjson.Number `json:"open_price"`
	ClosePrice xjson.Number `json:"close_price"`
	Volume     xjson.Number `json:"volume"`
	Timestamp  int64        `json:"timestamp"`
}

type bitmartKlineResponse struct {
	Code    int                `json:"code"`
	Message string             `json:"message"`
	Data    []bitmartKlineItem `json:"data"`
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]bitmartKlineItem, error) {
	params := map[string]string{
		"symbol": symbol,
		"step":   strconv.Itoa(mapBitmartInterval(interval)),
	}
	if !start.IsZero() {
		params["start_time"] = strconv.FormatInt(start.Unix(), 10)
	}
	if !end.IsZero() {
		params["end_time"] = strconv.FormatInt(end.Unix(), 10)
	}

	body, err := c.RawRequest(ctx, http.MethodGet, "/contract/public/kline", params, nil)
	if err != nil {
		return nil, err
	}

	var resp bitmartKlineResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal klines: %w", err)
	}
	if resp.Code != 1000 {
		return nil, fmt.Errorf("bitmart API error: %d - %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// FetchKlines fetches public K-lines for bitmart.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("bitmart fetch klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(rawKlines))
	for _, k := range rawKlines {
		klines = append(klines, exchange.Kline{
			Timestamp: k.Timestamp * 1000,
			Open:      xjson.ToFloat64(k.OpenPrice),
			High:      xjson.ToFloat64(k.HighPrice),
			Low:       xjson.ToFloat64(k.LowPrice),
			Close:     xjson.ToFloat64(k.ClosePrice),
			Volume:    xjson.ToFloat64(k.Volume),
		})
	}

	return klines, nil
}
