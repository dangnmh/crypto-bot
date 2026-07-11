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

var intervalDurations = map[exchange.Interval]time.Duration{
	exchange.Interval1m:  time.Minute,
	exchange.Interval3m:  3 * time.Minute,
	exchange.Interval5m:  5 * time.Minute,
	exchange.Interval15m: 15 * time.Minute,
	exchange.Interval30m: 30 * time.Minute,
	exchange.Interval1h:  time.Hour,
	exchange.Interval2h:  2 * time.Hour,
	exchange.Interval4h:  4 * time.Hour,
	exchange.Interval6h:  6 * time.Hour,
	exchange.Interval8h:  8 * time.Hour,
	exchange.Interval12h: 12 * time.Hour,
	exchange.Interval1d:  24 * time.Hour,
	exchange.Interval1w:  7 * 24 * time.Hour,
	exchange.Interval1M:  30 * 24 * time.Hour,
}

func mapOrangexInterval(interval exchange.Interval) string {
	if val, ok := intervalMap[interval]; ok {
		return val
	}
	return "1"
}

type orangexKlineItem struct {
	Open   xjson.Number `json:"open"`
	Close  xjson.Number `json:"close"`
	High   xjson.Number `json:"high"`
	Low    xjson.Number `json:"low"`
	Tick   int64        `json:"tick"`
	Volume xjson.Number `json:"volume"`
	Cost   xjson.Number `json:"cost"`
}

func (c *Client) rawGetKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]orangexKlineItem, error) {
	now := c.clock.Now()
	startTime := start
	endTime := end

	if startTime.IsZero() {
		startTime = now.Add(-24 * time.Hour)
	}
	if endTime.IsZero() {
		endTime = now
	}

	candleDur := intervalDurations[interval]
	if candleDur <= 0 {
		candleDur = time.Minute
	}

	maxDuration := 1500 * candleDur
	if endTime.Sub(startTime) > maxDuration {
		startTime = endTime.Add(-maxDuration)
	}

	params := map[string]any{
		paramInstrument:   symbol,
		"resolution":      mapOrangexInterval(interval),
		"start_timestamp": startTime.UnixMilli(),
		"end_timestamp":   endTime.UnixMilli(),
	}

	path := "/private/get_tradingview_chart_data"
	signed := true
	body, err := c.postRPC(ctx, path, path, params, signed)
	if err != nil {
		return nil, err
	}

	var envelope orangexRPCResponse[[]orangexKlineItem]
	if err := xjson.Unmarshal(body, &envelope); err != nil {
		return nil, fmt.Errorf("unmarshal klines: %w", err)
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return envelope.Result, nil
}

// FetchKlines fetches public K-lines for orangex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	res, err := c.rawGetKlines(ctx, symbol, interval, start, end)
	if err != nil {
		return nil, fmt.Errorf("orangex fetch klines: %w", err)
	}

	n := len(res)
	if n == 0 {
		return nil, nil
	}

	klines := make([]exchange.Kline, 0, n)
	for i := range n {
		item := &res[i]
		klines = append(klines, exchange.Kline{
			Timestamp: item.Tick * 1000,
			Open:      xjson.ToFloat64(item.Open),
			High:      xjson.ToFloat64(item.High),
			Low:       xjson.ToFloat64(item.Low),
			Close:     xjson.ToFloat64(item.Close),
			Volume:    xjson.ToFloat64(item.Volume),
		})
	}

	return klines, nil
}
