package kucoin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

func parseJSONFloat(val any) float64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return v
	case string:
		f, _ := strconv.ParseFloat(v, 64)
		return f
	case int64:
		return float64(v)
	case int:
		return float64(v)
	default:
		return 0
	}
}

func parseJSONInt(val any) int64 {
	if val == nil {
		return 0
	}
	switch v := val.(type) {
	case float64:
		return int64(v)
	case string:
		i, _ := strconv.ParseInt(v, 10, 64)
		return i
	case int64:
		return v
	case int:
		return int64(v)
	default:
		return 0
	}
}

func mapKucoinInterval(interval exchange.Interval) string {
	switch interval {
	case exchange.Interval1m:
		return "1"
	case exchange.Interval3m:
		return "3"
	case exchange.Interval5m:
		return "5"
	case exchange.Interval15m:
		return "15"
	case exchange.Interval30m:
		return "30"
	case exchange.Interval1h:
		return "60"
	case exchange.Interval2h:
		return "120"
	case exchange.Interval4h:
		return "240"
	case exchange.Interval6h:
		return "360"
	case exchange.Interval8h:
		return "480"
	case exchange.Interval12h:
		return "720"
	case exchange.Interval1d:
		return "1440"
	case exchange.Interval1w:
		return "10080"
	case exchange.Interval1M:
		return "43200"
	default:
		return "1"
	}
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]any, error) {
	params := map[string]string{
		paramSymbol:   symbol,
		"granularity": mapKucoinInterval(interval),
	}
	if !start.IsZero() {
		params["from"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if !end.IsZero() {
		params["to"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.RawRequest(ctx, http.MethodGet, pathKlines, params, nil)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[][]any](body, "klines")
}

// FetchKlines fetches public K-lines for kucoin.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("kucoin fetch klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(rawKlines))
	for _, k := range rawKlines {
		if len(k) < 6 {
			continue
		}
		klines = append(klines, exchange.Kline{
			Timestamp: parseJSONInt(k[0]),
			Open:      parseJSONFloat(k[1]),
			High:      parseJSONFloat(k[2]),
			Low:       parseJSONFloat(k[3]),
			Close:     parseJSONFloat(k[4]),
			Volume:    parseJSONFloat(k[5]),
		})
	}

	return klines, nil
}
