package zoomex

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

var intervalMap = map[exchange.Interval]string{
	exchange.Interval1m:  "1",
	exchange.Interval3m:  "3",
	exchange.Interval5m:  "5",
	exchange.Interval15m: "15",
	exchange.Interval30m: "30",
	exchange.Interval1h:  "60",
	exchange.Interval2h:  "120",
	exchange.Interval4h:  "240",
	exchange.Interval6h:  "360",
	exchange.Interval8h:  "360",
	exchange.Interval12h: "720",
	exchange.Interval1d:  "D",
	exchange.Interval1w:  "W",
	exchange.Interval1M:  "M",
}

func mapZoomexInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1"
}

type zoomexKlinesResult struct {
	List [][]xjson.Number `json:"list"`
}

type zoomexKlinesResponse struct {
	RetCode int64              `json:"retCode"`
	RetMsg  string             `json:"retMsg"`
	Result  zoomexKlinesResult `json:"result"`
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
	params := map[string]string{
		"symbol":   symbol,
		"category": categoryLinear,
		"interval": mapZoomexInterval(interval),
	}
	if !start.IsZero() {
		params["start"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if !end.IsZero() {
		params["end"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.request(ctx, http.MethodGet, "/cloud/trade/v3/market/kline", params)
	if err != nil {
		return nil, err
	}

	var resp zoomexKlinesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal zoomex klines: %w", err)
	}

	if resp.RetCode != 0 {
		return nil, fmt.Errorf("zoomex API error: %d - %s", resp.RetCode, resp.RetMsg)
	}

	return resp.Result.List, nil
}

// FetchKlines fetches public K-lines for zoomex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("zoomex fetch klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(rawKlines))
	for _, k := range rawKlines {
		if len(k) < 6 {
			continue
		}
		klines = append(klines, exchange.Kline{
			Timestamp: xjson.ToInt64(k[0]),
			Open:      xjson.ToFloat64(k[1]),
			High:      xjson.ToFloat64(k[2]),
			Low:       xjson.ToFloat64(k[3]),
			Close:     xjson.ToFloat64(k[4]),
			Volume:    xjson.ToFloat64(k[5]),
		})
	}

	return klines, nil
}
