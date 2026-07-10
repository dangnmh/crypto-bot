package fameex

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type fameexKlineItem struct {
	Idx   int64        `json:"idx"`
	Open  xjson.Number `json:"open"`
	High  xjson.Number `json:"high"`
	Low   xjson.Number `json:"low"`
	Close xjson.Number `json:"close"`
	Vol   xjson.Number `json:"vol"`
}

type fameexKlinesResponse struct {
	Code int               `json:"code"`
	Msg  string            `json:"msg"`
	Data []fameexKlineItem `json:"data"`
	Succ bool              `json:"succ"`
}

const (
	fameex1m  = "1min"
	fameex3m  = "3min"
	fameex5m  = "5min"
	fameex15m = "15min"
	fameex30m = "30min"
	fameex1h  = "60min"
	fameex1d  = "1day"
	fameex1w  = "1week"
	fameex1M  = "1month"
)

var fameexIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  fameex1m,
	exchange.Interval3m:  fameex3m,
	exchange.Interval5m:  fameex5m,
	exchange.Interval15m: fameex15m,
	exchange.Interval30m: fameex30m,
	exchange.Interval1h:  fameex1h,
	exchange.Interval1d:  fameex1d,
	exchange.Interval1w:  fameex1w,
	exchange.Interval1M:  fameex1M,
}

func mapFameexInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := fameexIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for FameEX.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToUpper(symbol)
	cleanSymbol = strings.ReplaceAll(cleanSymbol, "_", "")
	cleanSymbol = strings.ReplaceAll(cleanSymbol, "-", "")

	mappedInterval, err := mapFameexInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("fameex interval map: %w", err)
	}

	targetBase := strings.Replace(c.baseURL, "api.fameex.com", "openapi.fameex.com", 1)

	reqURL, err := url.Parse(targetBase + "/sapi/v1/klines")
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	q := reqURL.Query()
	q.Set("symbol", cleanSymbol)
	q.Set("interval", mappedInterval)

	if !start.IsZero() {
		q.Set("startTime", fmt.Sprintf("%d", start.UnixMilli()))
	}
	if !end.IsZero() {
		q.Set("endTime", fmt.Sprintf("%d", end.UnixMilli()))
	}
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fameex request klines: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res fameexKlinesResponse
	if err := xjson.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("fameex unmarshal klines: %w", err)
	}

	if !res.Succ || res.Code != 0 {
		return nil, fmt.Errorf("fameex API error: code=%d msg=%s", res.Code, res.Msg)
	}

	klines := make([]exchange.Kline, 0, len(res.Data))
	for i := range res.Data {
		item := &res.Data[i]
		o, _ := item.Open.Float64()
		h, _ := item.High.Float64()
		l, _ := item.Low.Float64()
		cVal, _ := item.Close.Float64()
		vol, _ := item.Vol.Float64()

		klines = append(klines, exchange.Kline{
			Timestamp: item.Idx,
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
