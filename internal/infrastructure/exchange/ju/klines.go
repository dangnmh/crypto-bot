package ju

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type juKlineItem struct {
	T int64        `json:"t"`
	O xjson.Number `json:"o"`
	H xjson.Number `json:"h"`
	L xjson.Number `json:"l"`
	C xjson.Number `json:"c"`
	Q xjson.Number `json:"q"` // base volume
	V xjson.Number `json:"v"` // quote volume (turnover)
}

type juKlinesResponse struct {
	Code int           `json:"code"`
	Msg  string        `json:"msg"`
	Data []juKlineItem `json:"data"`
}

var juIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  "1m",
	exchange.Interval3m:  "3m",
	exchange.Interval5m:  "5m",
	exchange.Interval15m: "15m",
	exchange.Interval30m: "30m",
	exchange.Interval1h:  "1h",
	exchange.Interval2h:  "2h",
	exchange.Interval4h:  "4h",
	exchange.Interval6h:  "6h",
	exchange.Interval8h:  "8h",
	exchange.Interval12h: "12h",
	exchange.Interval1d:  "1d",
	exchange.Interval1w:  "1w",
	exchange.Interval1M:  "1M",
}

func mapJuInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := juIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for Jucoin.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	if !strings.Contains(cleanSymbol, "_") {
		cleanSymbol = strings.ReplaceAll(cleanSymbol, "-", "_")
		if !strings.Contains(cleanSymbol, "_") {
			if before, ok := strings.CutSuffix(cleanSymbol, "USDT"); ok {
				cleanSymbol = before + "_USDT"
			} else if before, ok := strings.CutSuffix(cleanSymbol, "USDC"); ok {
				cleanSymbol = before + "_USDC"
			} else {
				cleanSymbol += "_USDT"
			}
		}
	}

	mappedInterval, err := mapJuInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("ju interval map: %w", err)
	}

	path := "/v1/spot/public/kline"
	var qParts []string
	qParts = append(qParts,
		"symbol="+url.QueryEscape(cleanSymbol),
		"interval="+url.QueryEscape(mappedInterval),
	)

	if !start.IsZero() {
		qParts = append(qParts, fmt.Sprintf("startTime=%d", start.UnixMilli()))
	}
	if !end.IsZero() {
		qParts = append(qParts, fmt.Sprintf("endTime=%d", end.UnixMilli()))
	}

	if len(qParts) > 0 {
		path += "?" + strings.Join(qParts, "&")
	}

	body, err := c.request(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("ju request klines: %w", err)
	}

	var resp juKlinesResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("ju unmarshal klines: %w", err)
	}

	if resp.Code != 200 {
		return nil, fmt.Errorf("ju API error: code=%d msg=%s", resp.Code, resp.Msg)
	}

	klines := make([]exchange.Kline, 0, len(resp.Data))
	for i := range resp.Data {
		item := &resp.Data[i]
		o, _ := item.O.Float64()
		h, _ := item.H.Float64()
		l, _ := item.L.Float64()
		cVal, _ := item.C.Float64()
		vol, _ := item.Q.Float64()
		amt, _ := item.V.Float64()

		klines = append(klines, exchange.Kline{
			Timestamp: item.T,
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
