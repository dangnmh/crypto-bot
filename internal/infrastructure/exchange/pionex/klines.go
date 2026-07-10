package pionex

import (
	"context"
	"fmt"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type pionexKlineItem struct {
	Time   int64        `json:"time"`
	Open   xjson.Number `json:"open"`
	Close  xjson.Number `json:"close"`
	High   xjson.Number `json:"high"`
	Low    xjson.Number `json:"low"`
	Volume xjson.Number `json:"volume"`
}

type pionexKlineResponse struct {
	Result bool                    `json:"result"`
	Data   pionexKlineResponseData `json:"data"`
}

type pionexKlineResponseData struct {
	Klines []pionexKlineItem `json:"klines"`
}

const (
	interval1m  = "1M"
	interval5m  = "5M"
	interval15m = "15M"
	interval30m = "30M"
	interval60m = "60M"
	interval4h  = "4H"
	interval8h  = "8H"
	interval12h = "12H"
	interval1d  = "1D"
)

var pionexIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  interval1m,
	exchange.Interval5m:  interval5m,
	exchange.Interval15m: interval15m,
	exchange.Interval30m: interval30m,
	exchange.Interval1h:  interval60m,
	exchange.Interval4h:  interval4h,
	exchange.Interval8h:  interval8h,
	exchange.Interval12h: interval12h,
	exchange.Interval1d:  interval1d,
	// Fallbacks
	exchange.Interval3m: interval1m,
	exchange.Interval2h: interval60m,
	exchange.Interval6h: interval4h,
	exchange.Interval1w: interval1d,
	exchange.Interval1M: interval1d,
}

func mapPionexInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := pionexIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for pionex.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	if !strings.HasSuffix(cleanSymbol, "_PERP") {
		cleanSymbol = strings.ReplaceAll(cleanSymbol, "_", "")
		if before, ok := strings.CutSuffix(cleanSymbol, "USDT"); ok {
			cleanSymbol = before + "_USDT_PERP"
		} else if before, ok := strings.CutSuffix(cleanSymbol, "USDC"); ok {
			cleanSymbol = before + "_USDC_PERP"
		} else {
			cleanSymbol += "_PERP"
		}
	}

	mappedInterval, err := mapPionexInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("pionex interval map: %w", err)
	}

	params := map[string]string{
		"symbol":   cleanSymbol,
		"interval": mappedInterval,
	}

	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.rawRequestPublic(ctx, "GET", "/api/v1/market/klines", params)
	if err != nil {
		return nil, fmt.Errorf("pionex request klines: %w", err)
	}

	var resp pionexKlineResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("pionex unmarshal klines: %w", err)
	}

	if !resp.Result {
		return nil, fmt.Errorf("pionex API error: klines retrieval failed")
	}

	klines := make([]exchange.Kline, 0, len(resp.Data.Klines))
	for i := range resp.Data.Klines {
		item := &resp.Data.Klines[i]
		cVal := xjson.ToFloat64(item.Close)
		volVal := xjson.ToFloat64(item.Volume)
		amountVal := volVal * cVal

		klines = append(klines, exchange.Kline{
			Timestamp: item.Time,
			Open:      xjson.ToFloat64(item.Open),
			High:      xjson.ToFloat64(item.High),
			Low:       xjson.ToFloat64(item.Low),
			Close:     cVal,
			Volume:    volVal,
			Amount:    amountVal,
		})
	}

	return klines, nil
}
