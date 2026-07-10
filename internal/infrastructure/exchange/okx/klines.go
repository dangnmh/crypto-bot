package okx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

const (
	bar1m  = "1m"
	bar3m  = "3m"
	bar5m  = "5m"
	bar15m = "15m"
	bar30m = "30m"
	bar1h  = "1H"
	bar2h  = "2H"
	bar4h  = "4H"
	bar6h  = "6H"
	bar12h = "12H"
	bar1d  = "1D"
	bar1w  = "1W"
	bar1M  = "1M"
)

var okxIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  bar1m,
	exchange.Interval3m:  bar3m,
	exchange.Interval5m:  bar5m,
	exchange.Interval15m: bar15m,
	exchange.Interval30m: bar30m,
	exchange.Interval1h:  bar1h,
	exchange.Interval2h:  bar2h,
	exchange.Interval4h:  bar4h,
	exchange.Interval6h:  bar6h,
	exchange.Interval12h: bar12h,
	exchange.Interval1d:  bar1d,
	exchange.Interval1w:  bar1w,
	exchange.Interval1M:  bar1M,
	// Fallbacks
	exchange.Interval8h: bar6h,
}

func mapOKXInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := okxIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for OKX.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	if !strings.HasSuffix(cleanSymbol, "-SWAP") {
		cleanSymbol = strings.ReplaceAll(cleanSymbol, "_", "-")
		if !strings.Contains(cleanSymbol, "-") {
			if before, ok := strings.CutSuffix(cleanSymbol, "USDT"); ok {
				cleanSymbol = before + "-USDT-SWAP"
			} else if before, ok := strings.CutSuffix(cleanSymbol, "USDC"); ok {
				cleanSymbol = before + "-USDC-SWAP"
			} else {
				cleanSymbol += "-USDT-SWAP"
			}
		} else {
			cleanSymbol += "-SWAP"
		}
	}

	mappedInterval, err := mapOKXInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("okx interval map: %w", err)
	}

	params := map[string]string{
		paramInstId: cleanSymbol,
		"bar":       mappedInterval,
	}

	if !start.IsZero() {
		params["before"] = fmt.Sprintf("%d", start.UnixMilli()-1)
	}
	if !end.IsZero() {
		params["after"] = fmt.Sprintf("%d", end.UnixMilli()+1)
	}

	body, err := c.RawRequest(ctx, "GET", "/api/v5/market/candles", params, nil)
	if err != nil {
		return nil, fmt.Errorf("okx request klines: %w", err)
	}

	candlesData, err := ParseResponse[[]string](body, "candles")
	if err != nil {
		return nil, fmt.Errorf("okx parse klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(candlesData))
	for _, item := range candlesData {
		if len(item) < 8 {
			continue
		}

		ts, _ := strconv.ParseInt(item[0], 10, 64)
		o, _ := strconv.ParseFloat(item[1], 64)
		h, _ := strconv.ParseFloat(item[2], 64)
		l, _ := strconv.ParseFloat(item[3], 64)
		cVal, _ := strconv.ParseFloat(item[4], 64)
		volCcy, _ := strconv.ParseFloat(item[6], 64)      // volume in base coin
		volCcyQuote, _ := strconv.ParseFloat(item[7], 64) // volume in quote currency (USD)

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
