package weex

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

const (
	weex1m  = "1m"
	weex5m  = "5m"
	weex15m = "15m"
	weex30m = "30m"
	weex1h  = "1h"
	weex4h  = "4h"
	weex12h = "12h"
	weex1d  = "1d"
	weex1w  = "1w"
)

var weexIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  weex1m,
	exchange.Interval5m:  weex5m,
	exchange.Interval15m: weex15m,
	exchange.Interval30m: weex30m,
	exchange.Interval1h:  weex1h,
	exchange.Interval4h:  weex4h,
	exchange.Interval12h: weex12h,
	exchange.Interval1d:  weex1d,
	exchange.Interval1w:  weex1w,
	// Fallbacks
	exchange.Interval3m: weex1m,
	exchange.Interval2h: weex1h,
	exchange.Interval6h: weex4h,
	exchange.Interval8h: weex4h,
	exchange.Interval1M: weex1w,
}

func mapWEEXInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := weexIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

func parseFloatValue(v any) float64 {
	if v == nil {
		return 0
	}
	if s, ok := v.(string); ok {
		val, _ := strconv.ParseFloat(s, 64)
		return val
	}
	if f, ok := v.(float64); ok {
		return f
	}
	return 0
}

// FetchKlines fetches public K-lines for weex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(strings.ReplaceAll(symbol, "_", ""))

	mappedInterval, err := mapWEEXInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("weex interval map: %w", err)
	}

	params := map[string]string{
		"symbol":   cleanSymbol,
		"interval": mappedInterval,
	}

	if !start.IsZero() {
		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, "GET", "/capi/v3/market/historyKlines", params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("weex request klines: %w", err)
	}

	candlesData, err := parseResponse[[][]any](body)
	if err != nil {
		return nil, fmt.Errorf("weex parse klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(candlesData))
	for _, item := range candlesData {
		if len(item) < 8 {
			continue
		}

		var ts int64
		switch v := item[0].(type) {
		case float64:
			ts = int64(v)
		case string:
			val, _ := strconv.ParseInt(v, 10, 64)
			ts = val
		}

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      parseFloatValue(item[1]),
			High:      parseFloatValue(item[2]),
			Low:       parseFloatValue(item[3]),
			Close:     parseFloatValue(item[4]),
			Volume:    parseFloatValue(item[5]),
			Amount:    parseFloatValue(item[7]),
		})
	}

	return klines, nil
}
