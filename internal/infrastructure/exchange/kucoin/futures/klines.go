package futures

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
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
	exchange.Interval8h:  "480",
	exchange.Interval12h: "720",
	exchange.Interval1d:  "1440",
	exchange.Interval1w:  "10080",
	exchange.Interval1M:  "43200",
}

func mapKucoinInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1"
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
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

	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/kline/query", params, nil, false)
	if err != nil {
		return nil, err
	}

	return kucoin.ParseResponse[[][]xjson.Number](body, "klines")
}

// FetchKlines fetches public K-lines for kucoin futures.
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
