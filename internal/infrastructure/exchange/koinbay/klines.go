package koinbay

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type koinbayKlineItem struct {
	Idx   xjson.Number `json:"idx"`
	Open  xjson.Number `json:"open"`
	High  xjson.Number `json:"high"`
	Low   xjson.Number `json:"low"`
	Close xjson.Number `json:"close"`
	Vol   xjson.Number `json:"vol"`
}

type koinbayErrorResponse struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

const (
	koinbay1m  = "1min"
	koinbay3m  = "3min"
	koinbay5m  = "5min"
	koinbay15m = "15min"
	koinbay30m = "30min"
	koinbay1h  = "60min"
	koinbay2h  = "2h"
	koinbay4h  = "4h"
	koinbay6h  = "6h"
	koinbay12h = "12h"
	koinbay1d  = "1day"
	koinbay1w  = "1week"
	koinbay1M  = "1month"
)

var koinbayIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  koinbay1m,
	exchange.Interval3m:  koinbay3m,
	exchange.Interval5m:  koinbay5m,
	exchange.Interval15m: koinbay15m,
	exchange.Interval30m: koinbay30m,
	exchange.Interval1h:  koinbay1h,
	exchange.Interval2h:  koinbay2h,
	exchange.Interval4h:  koinbay4h,
	exchange.Interval6h:  koinbay6h,
	exchange.Interval12h: koinbay12h,
	exchange.Interval1d:  koinbay1d,
	exchange.Interval1w:  koinbay1w,
	exchange.Interval1M:  koinbay1M,
}

func mapKoinbayInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := koinbayIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for koinbay.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	if !strings.HasPrefix(cleanSymbol, "E-") {
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
		cleanSymbol = "E-" + cleanSymbol
	}

	mappedInterval, err := mapKoinbayInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("koinbay interval map: %w", err)
	}

	params := map[string]string{
		"contractName": cleanSymbol,
		"interval":     mappedInterval,
	}

	if !start.IsZero() {
		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, "/fapi/v1/klines", params)
	if err != nil {
		return nil, fmt.Errorf("koinbay request klines: %w", err)
	}

	var klinesData []koinbayKlineItem
	if err := json.Unmarshal(body, &klinesData); err != nil {
		var errResp koinbayErrorResponse
		if err2 := json.Unmarshal(body, &errResp); err2 == nil && errResp.Code != "" {
			return nil, fmt.Errorf("koinbay API error: code=%s msg=%s", errResp.Code, errResp.Msg)
		}
		return nil, fmt.Errorf("koinbay unmarshal klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(klinesData))
	for i := range klinesData {
		item := &klinesData[i]
		ts, _ := strconv.ParseInt(item.Idx.String(), 10, 64)
		o, _ := item.Open.Float64()
		h, _ := item.High.Float64()
		l, _ := item.Low.Float64()
		cVal, _ := item.Close.Float64()
		vol, _ := item.Vol.Float64()

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
