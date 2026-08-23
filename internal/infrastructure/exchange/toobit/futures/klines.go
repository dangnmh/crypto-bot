package futures

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
	exchange.Interval1m:  "1m",
	exchange.Interval3m:  "3m",
	exchange.Interval5m:  "5m",
	exchange.Interval15m: "15m",
	exchange.Interval30m: "30m",
	exchange.Interval1h:  "1h",
	exchange.Interval2h:  "2h",
	exchange.Interval4h:  "4h",
	exchange.Interval6h:  "6h",
	exchange.Interval8h:  "8h",
	exchange.Interval12h: "12h",
	exchange.Interval1d:  "1d",
	exchange.Interval1w:  "1w",
	exchange.Interval1M:  "1M",
}

func mapToobitInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1m"
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
	params := map[string]string{
		symbolKey:  symbol,
		"interval": mapToobitInterval(interval),
		limitKey:   "1000",
	}
	if !start.IsZero() {
		params["startTime"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if !end.IsZero() {
		params["endTime"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.base.Request(ctx, http.MethodGet, "/quote/v1/klines", params, false)
	if err != nil {
		return nil, err
	}

	var rawKlines [][]xjson.Number
	if err := xjson.Unmarshal(body, &rawKlines); err != nil {
		return nil, fmt.Errorf("unmarshal klines: %w", err)
	}
	return rawKlines, nil
}

// FetchKlines fetches public K-lines for toobit.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("toobit fetch klines: %w", err)
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
