package lbank

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type lbankKlinesResponse struct {
	Result    string          `json:"result"`
	Msg       string          `json:"msg"`
	ErrorCode int             `json:"error_code"`
	Data      [][]interface{} `json:"data"`
}

var lbankIntervals = map[exchange.Interval]string{
	exchange.Interval1m:  "minute1",
	exchange.Interval5m:  "minute5",
	exchange.Interval15m: "minute15",
	exchange.Interval30m: "minute30",
	exchange.Interval1h:  "hour1",
	exchange.Interval4h:  "hour4",
	exchange.Interval8h:  "hour8",
	exchange.Interval12h: "hour12",
	exchange.Interval1d:  "day1",
	exchange.Interval1w:  "week1",
}

func mapLbankInterval(interval exchange.Interval) (string, error) {
	if mapped, ok := lbankIntervals[interval]; ok {
		return mapped, nil
	}
	return "", fmt.Errorf("unsupported interval: %s", interval)
}

// FetchKlines fetches public K-lines for LBank.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	// Form symbol as lowercase with underscore (e.g. btc_usdt)
	cleanSymbol := strings.ToLower(symbol)
	if !strings.Contains(cleanSymbol, "_") {
		cleanSymbol = strings.ReplaceAll(cleanSymbol, "-", "_")
		if !strings.Contains(cleanSymbol, "_") {
			if before, ok := strings.CutSuffix(cleanSymbol, "usdt"); ok {
				cleanSymbol = before + "_usdt"
			} else if before, ok := strings.CutSuffix(cleanSymbol, "usdc"); ok {
				cleanSymbol = before + "_usdc"
			} else {
				cleanSymbol += "_usdt"
			}
		}
	}

	mappedInterval, err := mapLbankInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("lbank interval map: %w", err)
	}

	targetBase := strings.Replace(c.baseURL, "api.lbank.info", "api.lbkex.com", 1)

	reqURL, err := url.Parse(targetBase + "/v2/kline.do")
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}

	q := reqURL.Query()
	q.Set("symbol", cleanSymbol)
	q.Set("type", mappedInterval)

	startTimeSecs := time.Now().Add(-2 * time.Hour).Unix()
	if !start.IsZero() {
		startTimeSecs = start.Unix()
	}
	q.Set("time", fmt.Sprintf("%d", startTimeSecs))

	// Calculate size or default to 1000
	size := 1000
	if !end.IsZero() && !start.IsZero() {
		diff := end.Sub(start)
		var candleDuration time.Duration
		switch interval {
		case exchange.Interval1m:
			candleDuration = time.Minute
		case exchange.Interval5m:
			candleDuration = 5 * time.Minute
		case exchange.Interval15m:
			candleDuration = 15 * time.Minute
		case exchange.Interval30m:
			candleDuration = 30 * time.Minute
		case exchange.Interval1h:
			candleDuration = time.Hour
		case exchange.Interval4h:
			candleDuration = 4 * time.Hour
		case exchange.Interval8h:
			candleDuration = 8 * time.Hour
		case exchange.Interval12h:
			candleDuration = 12 * time.Hour
		case exchange.Interval1d:
			candleDuration = 24 * time.Hour
		case exchange.Interval1w:
			candleDuration = 7 * 24 * time.Hour
		default:
			candleDuration = time.Minute
		}
		size = int(diff / candleDuration)
		if size <= 0 {
			size = 1
		} else if size > 2000 {
			size = 2000
		}
	}
	q.Set("size", fmt.Sprintf("%d", size))
	reqURL.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL.String(), http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lbank request klines: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	var res lbankKlinesResponse
	if err := xjson.Unmarshal(body, &res); err != nil {
		return nil, fmt.Errorf("lbank unmarshal klines: %w", err)
	}

	if res.Result != "true" || res.ErrorCode != 0 {
		return nil, fmt.Errorf("lbank API error: code=%d msg=%s", res.ErrorCode, res.Msg)
	}

	klines := make([]exchange.Kline, 0, len(res.Data))
	for _, row := range res.Data {
		if len(row) < 6 {
			continue
		}

		var ts int64
		switch val := row[0].(type) {
		case float64:
			ts = int64(val) * 1000
		case string:
			parsed, _ := strconv.ParseInt(val, 10, 64)
			ts = parsed * 1000
		}

		var oVal, hVal, lVal, cVal, volVal float64
		oVal, _ = valToFloat(row[1])
		hVal, _ = valToFloat(row[2])
		lVal, _ = valToFloat(row[3])
		cVal, _ = valToFloat(row[4])
		volVal, _ = valToFloat(row[5])

		klines = append(klines, exchange.Kline{
			Timestamp: ts,
			Open:      oVal,
			High:      hVal,
			Low:       lVal,
			Close:     cVal,
			Volume:    volVal,
			Amount:    volVal * cVal,
		})
	}

	return klines, nil
}

func valToFloat(val interface{}) (float64, error) {
	switch v := val.(type) {
	case float64:
		return v, nil
	case string:
		return strconv.ParseFloat(v, 64)
	default:
		return 0, fmt.Errorf("unknown float type")
	}
}
