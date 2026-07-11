package krakenfutures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

var krakenIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  "1m",
	exchange.Interval3m:  "1m",
	exchange.Interval5m:  "5m",
	exchange.Interval15m: "15m",
	exchange.Interval30m: "30m",
	exchange.Interval1h:  "1h",
	exchange.Interval2h:  "1h",
	exchange.Interval4h:  "4h",
	exchange.Interval6h:  "4h",
	exchange.Interval8h:  "4h",
	exchange.Interval12h: "12h",
	exchange.Interval1d:  "1d",
	exchange.Interval1w:  "1w",
	exchange.Interval1M:  "1d",
}

func mapKrakenInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := krakenIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

func toKrakenSymbol(symbol string) string {
	upper := strings.ToUpper(symbol)
	upper = strings.ReplaceAll(upper, "BTC", "XBT")
	if !strings.HasPrefix(upper, "PF_") && !strings.HasPrefix(upper, "PI_") {
		upper = "PF_" + upper
	}
	return upper
}

type krakenCandlesResponse struct {
	Candles []krakenCandle `json:"candles"`
}

type krakenCandle struct {
	Time   int64        `json:"time"`
	Open   xjson.Number `json:"open"`
	High   xjson.Number `json:"high"`
	Low    xjson.Number `json:"low"`
	Close  xjson.Number `json:"close"`
	Volume xjson.Number `json:"volume"`
}

// FetchKlines fetches public K-lines for krakenfutures.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	krakenSymbol := toKrakenSymbol(symbol)
	mappedInterval, err := mapKrakenInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("krakenfutures interval map: %w", err)
	}

	query := make(map[string]string)
	if !start.IsZero() {
		query["from"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		query["to"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	path := fmt.Sprintf("/api/charts/v1/trade/%s/%s", krakenSymbol, mappedInterval)
	body, err := c.request(ctx, http.MethodGet, path, query)
	if err != nil {
		return nil, fmt.Errorf("krakenfutures request: %w", err)
	}

	var resp krakenCandlesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("krakenfutures unmarshal: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(resp.Candles))
	for _, candle := range resp.Candles {
		klines = append(klines, exchange.Kline{
			Timestamp: candle.Time,
			Open:      xjson.ToFloat64(candle.Open),
			High:      xjson.ToFloat64(candle.High),
			Low:       xjson.ToFloat64(candle.Low),
			Close:     xjson.ToFloat64(candle.Close),
			Volume:    xjson.ToFloat64(candle.Volume),
		})
	}

	sort.Slice(klines, func(i, j int) bool {
		return klines[i].Timestamp < klines[j].Timestamp
	})

	return klines, nil
}
