package bydfi

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bydfiKlineItem struct {
	S string       `json:"s"`
	T xjson.Number `json:"t"`
	C xjson.Number `json:"c"`
	O xjson.Number `json:"o"`
	H xjson.Number `json:"h"`
	L xjson.Number `json:"l"`
	V xjson.Number `json:"v"`
}

type bydfiKlinesResponse struct {
	Code    int              `json:"code"`
	Message string           `json:"message"`
	Data    []bydfiKlineItem `json:"data"`
}

const (
	bydfi1m  = "1m"
	bydfi3m  = "3m"
	bydfi5m  = "5m"
	bydfi15m = "15m"
	bydfi30m = "30m"
	bydfi1h  = "1h"
	bydfi2h  = "2h"
	bydfi4h  = "4h"
	bydfi6h  = "6h"
	bydfi8h  = "8h"
	bydfi12h = "12h"
	bydfi1d  = "1d"
	bydfi1w  = "1w"
	bydfi1M  = "1M"
)

var bydfiIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  bydfi1m,
	exchange.Interval3m:  bydfi3m,
	exchange.Interval5m:  bydfi5m,
	exchange.Interval15m: bydfi15m,
	exchange.Interval30m: bydfi30m,
	exchange.Interval1h:  bydfi1h,
	exchange.Interval2h:  bydfi2h,
	exchange.Interval4h:  bydfi4h,
	exchange.Interval6h:  bydfi6h,
	exchange.Interval8h:  bydfi8h,
	exchange.Interval12h: bydfi12h,
	exchange.Interval1d:  bydfi1d,
	exchange.Interval1w:  bydfi1w,
	exchange.Interval1M:  bydfi1M,
}

func mapBydfiInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := bydfiIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for BYDFi.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	if !strings.Contains(cleanSymbol, "-") {
		cleanSymbol = strings.ReplaceAll(cleanSymbol, "_", "-")
		if !strings.Contains(cleanSymbol, "-") {
			if before, ok := strings.CutSuffix(cleanSymbol, "USDT"); ok {
				cleanSymbol = before + "-USDT"
			} else if before, ok := strings.CutSuffix(cleanSymbol, "USDC"); ok {
				cleanSymbol = before + "-USDC"
			} else {
				cleanSymbol += "-USDT"
			}
		}
	}

	mappedInterval, err := mapBydfiInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("bydfi interval map: %w", err)
	}

	params := map[string]string{
		"symbol":   cleanSymbol,
		"interval": mappedInterval,
	}

	if !start.IsZero() {
		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, "/v1/fapi/market/klines", params)
	if err != nil {
		return nil, fmt.Errorf("bydfi request klines: %w", err)
	}

	var resp bydfiKlinesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("bydfi unmarshal klines: %w", err)
	}

	if resp.Code != 200 {
		return nil, fmt.Errorf("bydfi API error: code=%d msg=%s", resp.Code, resp.Message)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		ts, _ := strconv.ParseInt(item.T.String(), 10, 64)
		o, _ := item.O.Float64()
		h, _ := item.H.Float64()
		l, _ := item.L.Float64()
		cVal, _ := item.C.Float64()
		vol, _ := item.V.Float64()

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      o,
			High:      h,
			Low:       l,
			Close:     cVal,
			Volume:    vol,
			Amount:    vol * cVal,
		})
	}

	return klines, nil
}
