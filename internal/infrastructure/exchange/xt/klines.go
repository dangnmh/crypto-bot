package xt

import (
	"context"
	"fmt"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type xtKlineItem struct {
	Open     xjson.Number `json:"o"`
	High     xjson.Number `json:"h"`
	Low      xjson.Number `json:"l"`
	Close    xjson.Number `json:"c"`
	Volume   xjson.Number `json:"a"` // contract amount count
	QuoteVol xjson.Number `json:"v"` // quote currency volume (turnover)
	Time     int64        `json:"t"`
}

type xtKlineResponse struct {
	ReturnCode int64         `json:"returnCode"`
	MsgInfo    string        `json:"msgInfo"`
	Result     []xtKlineItem `json:"result"`
}

func mapXTInterval(interval exchange.Interval) (string, error) {
	switch interval {
	case exchange.Interval1m:
		return "1m", nil
	case exchange.Interval5m:
		return "5m", nil
	case exchange.Interval15m:
		return "15m", nil
	case exchange.Interval30m:
		return "30m", nil
	case exchange.Interval1h:
		return "1h", nil
	case exchange.Interval4h:
		return "4h", nil
	case exchange.Interval1d:
		return "1d", nil
	case exchange.Interval1w:
		return "1w", nil
	case exchange.Interval1M:
		return "1M", nil
	default:
		switch interval {
		case exchange.Interval3m:
			return "1m", nil
		case exchange.Interval2h:
			return "1h", nil
		case exchange.Interval6h, exchange.Interval8h, exchange.Interval12h:
			return "4h", nil
		default:
			return "", fmt.Errorf("unsupported interval: %s", interval)
		}
	}
}

// FetchKlines fetches public K-lines for xt.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	cleanSymbol := strings.ToLower(symbol)
	if !strings.Contains(cleanSymbol, "_") {
		if before, ok := strings.CutSuffix(cleanSymbol, "usdt"); ok {
			cleanSymbol = before + "_usdt"
		} else if before, ok := strings.CutSuffix(cleanSymbol, "usdc"); ok {
			cleanSymbol = before + "_usdc"
		}
	}

	mappedInterval, err := mapXTInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("xt interval map: %w", err)
	}

	params := map[string]string{
		"symbol":   cleanSymbol,
		"interval": mappedInterval,
		"limit":    "100",
	}

	if !start.IsZero() {
		params["startTime"] = fmt.Sprintf("%d", start.UnixMilli())
	}
	if !end.IsZero() {
		params["endTime"] = fmt.Sprintf("%d", end.UnixMilli())
	}

	body, err := c.request(ctx, "GET", "/future/market/v1/public/q/kline", params, nil, false)
	if err != nil {
		return nil, fmt.Errorf("xt request klines: %w", err)
	}

	var resp xtKlineResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("xt unmarshal klines: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("xt API error: code=%d msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	klines := make([]exchange.Kline, 0, len(resp.Result))
	for i := range resp.Result {
		item := &resp.Result[i]
		cVal := xjson.ToFloat64(item.Close)
		qVol := xjson.ToFloat64(item.QuoteVol)

		var baseVol float64
		if cVal > 0 {
			baseVol = qVol / cVal
		}

		klines = append(klines, exchange.Kline{
			Timestamp: item.Time,
			Open:      xjson.ToFloat64(item.Open),
			High:      xjson.ToFloat64(item.High),
			Low:       xjson.ToFloat64(item.Low),
			Close:     cVal,
			Volume:    baseVol,
			Amount:    qVol,
		})
	}

	return klines, nil
}
