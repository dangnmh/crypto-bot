package batonex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// FetchKlines fetches public K-lines for batonex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	query := map[string]string{
		"symbol":   toBatonexSymbol(symbol),
		"interval": string(interval),
		"limit":    "100",
	}
	if !start.IsZero() {
		query["startTime"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if !end.IsZero() {
		query["endTime"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.request(ctx, http.MethodGet, "/openapi/quote/v1/contract/klines", query)
	if err != nil {
		return nil, fmt.Errorf("batonex fetch klines: %w", err)
	}

	var rawKlines [][]any
	if err := json.Unmarshal(body, &rawKlines); err != nil {
		return nil, fmt.Errorf("batonex unmarshal klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(rawKlines))
	for _, k := range rawKlines {
		if len(k) < 6 {
			continue
		}
		tsVal, ok := k[0].(float64)
		if !ok {
			continue
		}
		openStr, ok := k[1].(string)
		if !ok {
			continue
		}
		highStr, ok := k[2].(string)
		if !ok {
			continue
		}
		lowStr, ok := k[3].(string)
		if !ok {
			continue
		}
		closeStr, ok := k[4].(string)
		if !ok {
			continue
		}
		volStr, ok := k[5].(string)
		if !ok {
			continue
		}

		open, _ := strconv.ParseFloat(openStr, 64)
		high, _ := strconv.ParseFloat(highStr, 64)
		low, _ := strconv.ParseFloat(lowStr, 64)
		closeVal, _ := strconv.ParseFloat(closeStr, 64)
		volume, _ := strconv.ParseFloat(volStr, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: int64(tsVal),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    volume,
		})
	}

	return klines, nil
}
