package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type bingxKline struct {
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

type bingxKlinesResponse struct {
	Code int64        `json:"code"`
	Msg  string       `json:"msg"`
	Data []bingxKline `json:"data"`
}

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

func mapBingxInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1m"
}

// FetchKlines fetches public K-lines for bingx.
//
//nolint:goconst // JSON payload keys
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	params := map[string]string{
		paramSymbol: symbol,
		"interval":  mapBingxInterval(interval),
		"limit":     "100",
	}
	if !start.IsZero() {
		params["startTime"] = strconv.FormatInt(start.UnixMilli(), 10)
	}
	if !end.IsZero() {
		params["endTime"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.GetCtx(ctx, "/openApi/swap/v3/quote/klines", params)
	if err != nil {
		return nil, fmt.Errorf("bingx fetch klines: %w", err)
	}

	var resp bingxKlinesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bingx unmarshal klines: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("bingx fetch klines error code %d: %s", resp.Code, resp.Msg)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		open, _ := strconv.ParseFloat(item.Open, 64)
		high, _ := strconv.ParseFloat(item.High, 64)
		low, _ := strconv.ParseFloat(item.Low, 64)
		closeVal, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseFloat(item.Volume, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: item.Time,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    volume,
		})
	}

	return klines, nil
}
