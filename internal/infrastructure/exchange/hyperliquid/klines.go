package hyperliquid

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type hyperliquidCandle struct {
	OpenTime  int64  `json:"t"`
	CloseTime int64  `json:"T"`
	Open      string `json:"o"`
	High      string `json:"h"`
	Low       string `json:"l"`
	Close     string `json:"c"`
	Volume    string `json:"v"`
}

func mapHyperliquidInterval(interval exchange.Interval) string {
	switch interval {
	case exchange.Interval1m, exchange.Interval3m, exchange.Interval5m, exchange.Interval15m, exchange.Interval30m,
		exchange.Interval1h, exchange.Interval2h, exchange.Interval4h, exchange.Interval8h, exchange.Interval12h,
		exchange.Interval1d, exchange.Interval1w, exchange.Interval1M:
		return string(interval)
	case exchange.Interval6h:
		return "4h" // Fallback to nearest supported interval
	default:
		return "1m"
	}
}

// FetchKlines fetches public K-lines for hyperliquid.
//
//nolint:goconst // JSON request payload specifies "type"
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	reqBody := map[string]any{
		"type": "candleSnapshot",
		"req": map[string]any{
			"coin":      symbol,
			"interval":  mapHyperliquidInterval(interval),
			"startTime": start.UnixMilli(),
			"endTime":   end.UnixMilli(),
		},
	}

	bodyBytes, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid marshal klines request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/info", bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("hyperliquid create klines request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid fetch klines request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("hyperliquid klines API error status=%d: %s", resp.StatusCode, string(respBody))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid read klines response: %w", err)
	}

	var candles []hyperliquidCandle
	if err := xjson.Unmarshal(respBytes, &candles); err != nil {
		return nil, fmt.Errorf("hyperliquid unmarshal klines response: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(candles))
	for _, item := range candles {
		open, _ := strconv.ParseFloat(item.Open, 64)
		high, _ := strconv.ParseFloat(item.High, 64)
		low, _ := strconv.ParseFloat(item.Low, 64)
		closeVal, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseFloat(item.Volume, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: item.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    volume,
		})
	}

	return klines, nil
}
