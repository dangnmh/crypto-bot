package blofin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type blofinCandlesResponse struct {
	Code string     `json:"code"`
	Msg  string     `json:"msg"`
	Data [][]string `json:"data"`
}

const (
	bar1m  = "1m"
	bar5m  = "5m"
	bar15m = "15m"
	bar30m = "30m"
	bar1h  = "1H"
	bar4h  = "4H"
	bar1d  = "1D"
	bar1w  = "1W"
	bar1M  = "1M"
)

var blofinIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  bar1m,
	exchange.Interval5m:  bar5m,
	exchange.Interval15m: bar15m,
	exchange.Interval30m: bar30m,
	exchange.Interval1h:  bar1h,
	exchange.Interval4h:  bar4h,
	exchange.Interval1d:  bar1d,
	exchange.Interval1w:  bar1w,
	exchange.Interval1M:  bar1M,
	// Fallbacks
	exchange.Interval3m:  bar1m,
	exchange.Interval2h:  bar1h,
	exchange.Interval6h:  bar4h,
	exchange.Interval8h:  bar4h,
	exchange.Interval12h: bar4h,
}

func mapBlofinInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := blofinIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for Blofin.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	mappedInterval, err := mapBlofinInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("blofin interval map: %w", err)
	}

	params := map[string]string{
		"instId": symbol,
		"bar":    mappedInterval,
		"limit":  "100",
	}

	if !start.IsZero() {
		params["before"] = fmt.Sprintf("%d", start.UnixMilli())
	}

	if !end.IsZero() {
		params["after"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, "/api/v1/market/candles", params)
	if err != nil {
		return nil, fmt.Errorf("blofin request klines: %w", err)
	}

	var resp blofinCandlesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("blofin unmarshal klines: %w", err)
	}

	if resp.Code != "0" {
		return nil, fmt.Errorf("blofin API error: code=%s msg=%s", resp.Code, resp.Msg)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for _, item := range resp.Data {
		if len(item) < 8 {
			continue
		}

		ts, _ := strconv.ParseInt(item[0], 10, 64)
		o, _ := strconv.ParseFloat(item[1], 64)
		h, _ := strconv.ParseFloat(item[2], 64)
		l, _ := strconv.ParseFloat(item[3], 64)
		cVal, _ := strconv.ParseFloat(item[4], 64)
		volCcy, _ := strconv.ParseFloat(item[6], 64)      // volume in base currency
		volCcyQuote, _ := strconv.ParseFloat(item[7], 64) // volume in quote currency (USDT)

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     cVal,
			Volume:    volCcy,
			Amount:    volCcyQuote,
		})
	}

	return klines, nil
}
