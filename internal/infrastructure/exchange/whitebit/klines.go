package whitebit

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type whitebitKlinesResponse struct {
	Success bool    `json:"success"`
	Message string  `json:"message"`
	Result  [][]any `json:"result"`
}

const (
	wb1m  = "1m"
	wb3m  = "3m"
	wb5m  = "5m"
	wb15m = "15m"
	wb30m = "30m"
	wb1h  = "1h"
	wb2h  = "2h"
	wb4h  = "4h"
	wb6h  = "6h"
	wb8h  = "8h"
	wb12h = "12h"
	wb1d  = "1d"
	wb1w  = "1w"
)

var whitebitIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  wb1m,
	exchange.Interval3m:  wb3m,
	exchange.Interval5m:  wb5m,
	exchange.Interval15m: wb15m,
	exchange.Interval30m: wb30m,
	exchange.Interval1h:  wb1h,
	exchange.Interval2h:  wb2h,
	exchange.Interval4h:  wb4h,
	exchange.Interval6h:  wb6h,
	exchange.Interval8h:  wb8h,
	exchange.Interval12h: wb12h,
	exchange.Interval1d:  wb1d,
	exchange.Interval1w:  wb1w,
}

func mapWhitebitInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := whitebitIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for whitebit.
//
//nolint:cyclop // REST market methods are naturally complex
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	mappedInterval, err := mapWhitebitInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("whitebit interval map: %w", err)
	}

	path := "/api/v1/public/kline"
	var qParts []string
	qParts = append(qParts,
		"market="+url.QueryEscape(symbol),
		"interval="+url.QueryEscape(mappedInterval),
	)
	if !start.IsZero() {
		qParts = append(qParts, fmt.Sprintf("start=%d", start.Unix()))
	}
	if !end.IsZero() {
		qParts = append(qParts, fmt.Sprintf("end=%d", end.Unix()))
	}
	if len(qParts) > 0 {
		path += "?" + strings.Join(qParts, "&")
	}

	body, err := c.request(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("whitebit request klines: %w", err)
	}

	var resp whitebitKlinesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("whitebit unmarshal klines: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("whitebit API error: %s", resp.Message)
	}

	klines := make([]exchange.Kline, 0, len(resp.Result))
	for _, item := range resp.Result {
		if len(item) < 7 {
			continue
		}

		// WhiteBIT returns: [timestamp (int), open (str), close (str), high (str), low (str), volume (str), amount (str)]
		var tsVal int64
		switch v := item[0].(type) {
		case float64:
			tsVal = int64(v)
		case string:
			tsVal, _ = strconv.ParseInt(v, 10, 64)
		case int64:
			tsVal = v
		case int:
			tsVal = int64(v)
		}

		oStr := fmt.Sprintf("%v", item[1])
		cStr := fmt.Sprintf("%v", item[2])
		hStr := fmt.Sprintf("%v", item[3])
		lStr := fmt.Sprintf("%v", item[4])
		vStr := fmt.Sprintf("%v", item[5])
		aStr := fmt.Sprintf("%v", item[6])

		o, _ := strconv.ParseFloat(oStr, 64)
		cVal, _ := strconv.ParseFloat(cStr, 64)
		h, _ := strconv.ParseFloat(hStr, 64)
		l, _ := strconv.ParseFloat(lStr, 64)
		vol, _ := strconv.ParseFloat(vStr, 64)
		amt, _ := strconv.ParseFloat(aStr, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: tsVal * 1000, // convert seconds to milliseconds
			Open:      o,
			High:      h,
			Low:       l,
			Close:     cVal,
			Volume:    vol,
			Amount:    amt,
		})
	}

	return klines, nil
}
