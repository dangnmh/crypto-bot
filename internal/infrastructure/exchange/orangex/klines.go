package orangex

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
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
	exchange.Interval8h:  "360",
	exchange.Interval12h: "720",
	exchange.Interval1d:  "1D",
	exchange.Interval1w:  "1W",
	exchange.Interval1M:  "1M",
}

func mapOrangexInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1"
}

type orangexKlineResult struct {
	Volume []xjson.Number `json:"volume"`
	Ticks  []int64        `json:"ticks"`
	Status string         `json:"status"`
	Open   []xjson.Number `json:"open"`
	Low    []xjson.Number `json:"low"`
	High   []xjson.Number `json:"high"`
	Close  []xjson.Number `json:"close"`
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) (*orangexKlineResult, error) {
	params := map[string]any{
		paramInstrument: symbol,
		"resolution":    mapOrangexInterval(interval),
	}
	if !start.IsZero() {
		params["start_timestamp"] = start.UnixMilli()
	} else {
		params["start_timestamp"] = c.clock.Now().Add(-24 * time.Hour).UnixMilli()
	}
	if !end.IsZero() {
		params["end_timestamp"] = end.UnixMilli()
	} else {
		params["end_timestamp"] = c.clock.Now().UnixMilli()
	}

	body, err := c.postRPC(ctx, "/private/get_tradingview_chart_data", "/private/get_tradingview_chart_data", params, true)
	if err != nil {
		return nil, err
	}

	var envelope orangexRPCResponse[orangexKlineResult]
	if err := xjson.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal klines: %w", err)
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return &envelope.Result, nil
}

// FetchKlines fetches public K-lines for orangex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	res, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("orangex fetch klines: %w", err)
	}

	n := len(res.Ticks)
	if n == 0 {
		return nil, nil
	}

	if len(res.Open) < n || len(res.High) < n || len(res.Low) < n || len(res.Close) < n || len(res.Volume) < n {
		return nil, fmt.Errorf("invalid kline response: array length mismatch")
	}

	klines := make([]exchange.Kline, 0, n)
	for i := range n {
		klines = append(klines, exchange.Kline{
			Timestamp: res.Ticks[i],
			Open:      xjson.ToFloat64(res.Open[i]),
			High:      xjson.ToFloat64(res.High[i]),
			Low:       xjson.ToFloat64(res.Low[i]),
			Close:     xjson.ToFloat64(res.Close[i]),
			Volume:    xjson.ToFloat64(res.Volume[i]),
		})
	}

	return klines, nil
}
