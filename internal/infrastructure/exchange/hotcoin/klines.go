package hotcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type hotcoinKlineResponse struct {
	Code int     `json:"code"`
	Data [][]any `json:"data"`
	Msg  string  `json:"msg"`
}

const (
	kline1m  = "1min"
	kline5m  = "5min"
	kline15m = "15min"
	kline30m = "30min"
	kline1h  = "1hour"
	kline4h  = "4hour"
	kline1d  = "day"
	kline1w  = "week"
	kline1M  = "month"
)

var hotcoinIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  kline1m,
	exchange.Interval5m:  kline5m,
	exchange.Interval15m: kline15m,
	exchange.Interval30m: kline30m,
	exchange.Interval1h:  kline1h,
	exchange.Interval4h:  kline4h,
	exchange.Interval1d:  kline1d,
	exchange.Interval1w:  kline1w,
	exchange.Interval1M:  kline1M,
	// Fallbacks
	exchange.Interval3m:  kline1m,
	exchange.Interval2h:  kline1h,
	exchange.Interval6h:  kline4h,
	exchange.Interval8h:  kline4h,
	exchange.Interval12h: kline4h,
}

func mapHotcoinInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := hotcoinIntervals[interval]; ok {
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

// FetchKlines fetches public K-lines for hotcoin.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	contractCode := strings.ToUpper(strings.ReplaceAll(symbol, "_", ""))

	mappedInterval, err := mapHotcoinInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("hotcoin interval map: %w", err)
	}

	params := map[string]string{
		"kline": mappedInterval,
		"size":  "100",
	}

	if !end.IsZero() {
		params["since"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	path := fmt.Sprintf("/api/v1/perpetual/public/%s/candles", contractCode)
	body, err := c.request(ctx, "GET", path, params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("hotcoin request klines: %w", err)
	}

	var resp hotcoinKlineResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("hotcoin unmarshal klines: %w", err)
	}

	if resp.Code != 200 {
		return nil, fmt.Errorf("hotcoin API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for _, item := range resp.Data {
		if len(item) < 7 {
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
			Open:      parseFloatValue(item[3]),
			High:      parseFloatValue(item[2]),
			Low:       parseFloatValue(item[1]),
			Close:     parseFloatValue(item[4]),
			Volume:    parseFloatValue(item[5]),
			Amount:    parseFloatValue(item[6]),
		})
	}

	sort.Slice(klines, func(i, j int) bool {
		return klines[i].Timestamp < klines[j].Timestamp
	})

	return klines, nil
}
