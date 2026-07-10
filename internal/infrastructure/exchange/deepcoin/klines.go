package deepcoin

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
	exchange.Interval3m:  "1m",
	exchange.Interval5m:  "5m",
	exchange.Interval15m: "15m",
	exchange.Interval30m: "30m",
	exchange.Interval1h:  "1H",
	exchange.Interval2h:  "1H",
	exchange.Interval4h:  "4H",
	exchange.Interval6h:  "4H",
	exchange.Interval8h:  "4H",
	exchange.Interval12h: "12H",
	exchange.Interval1d:  "1D",
	exchange.Interval1w:  "1W",
	exchange.Interval1M:  "1M",
}

func mapDeepcoinInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1m"
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([][]xjson.Number, error) {
	params := map[string]string{
		paramInstId: symbol,
		"bar":       mapDeepcoinInterval(interval),
	}
	if !end.IsZero() {
		params["after"] = strconv.FormatInt(end.UnixMilli(), 10)
	}

	body, err := c.RawRequest(ctx, http.MethodGet, "/deepcoin/market/candles", params, nil)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]xjson.Number](body, "klines")
}

// FetchKlines fetches public K-lines for deepcoin.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	rawKlines, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("deepcoin fetch klines: %w", err)
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
