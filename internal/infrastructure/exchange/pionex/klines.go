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

func mapPionexInterval(interval exchange.Interval) (string, error) {
	switch interval {
	case exchange.Interval1m:
		return "1M", nil
	case exchange.Interval5m:
		return "5M", nil
	case exchange.Interval15m:
		return "15M", nil
	case exchange.Interval30m:
		return "30M", nil
	case exchange.Interval1h:
		return "60M", nil
	case exchange.Interval4h:
		return "4H", nil
	case exchange.Interval8h:
		return "8H", nil
	case exchange.Interval12h:
		return "12H", nil
	case exchange.Interval1d:
		return "1D", nil
	default:
		switch interval {
		case exchange.Interval3m:
			return "1M", nil
		case exchange.Interval2h:
			return "60M", nil
		case exchange.Interval6h:
			return "4H", nil
		case exchange.Interval1w, exchange.Interval1M:
			return "1D", nil
		default:
			return "", fmt.Errorf("unsupported interval: %s", interval)
		}
	}
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
			cleanSymbol = cleanSymbol + "_PERP"
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
