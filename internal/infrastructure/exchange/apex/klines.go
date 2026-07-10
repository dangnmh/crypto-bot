package apex

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type apexCandle struct {
	Timestamp int64  `json:"t"`
	Open      string `json:"o"`
	High      string `json:"h"`
	Low       string `json:"l"`
	Close     string `json:"c"`
	Volume    string `json:"v"`
}

type apexKlinesResponse struct {
	Data map[string][]apexCandle `json:"data"`
}

func mapApexInterval(interval exchange.Interval) string {
	switch interval {
	case exchange.Interval1m:
		return "1"
	case exchange.Interval3m:
		return "1"
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
	case exchange.Interval6h, exchange.Interval8h:
		return "240"
	case exchange.Interval12h:
		return "720"
	case exchange.Interval1d:
		return "D"
	case exchange.Interval1w:
		return "W"
	case exchange.Interval1M:
		return "M"
	default:
		return "1"
	}
}

// FetchKlines fetches public K-lines for apex.
//
//nolint:goconst // JSON payload keys
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", mapApexInterval(interval))
	params.Set("limit", "100")
	if !start.IsZero() {
		params.Set("start", strconv.FormatInt(start.UnixMilli(), 10))
	}
	if !end.IsZero() {
		params.Set("end", strconv.FormatInt(end.UnixMilli(), 10))
	}

	path := "/api/v3/klines?" + params.Encode()
	body, err := c.request(ctx, http.MethodGet, path)
	if err != nil {
		return nil, fmt.Errorf("apex fetch klines: %w", err)
	}

	var resp apexKlinesResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("apex unmarshal klines: %w", err)
	}

	candles, ok := resp.Data[symbol]
	if !ok || len(candles) == 0 {
		return nil, nil
	}

	klines := make([]exchange.Kline, 0, len(candles))
	for _, item := range candles {
		open, _ := strconv.ParseFloat(item.Open, 64)
		high, _ := strconv.ParseFloat(item.High, 64)
		low, _ := strconv.ParseFloat(item.Low, 64)
		closeVal, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseFloat(item.Volume, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: item.Timestamp,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    volume,
		})
	}

	return klines, nil
}
