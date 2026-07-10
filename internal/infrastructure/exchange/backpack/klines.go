package backpack

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type backpackCandle struct {
	Close       string `json:"close"`
	End         string `json:"end"`
	High        string `json:"high"`
	Low         string `json:"low"`
	Open        string `json:"open"`
	QuoteVolume string `json:"quoteVolume"`
	Start       string `json:"start"`
	Trades      string `json:"trades"`
	Volume      string `json:"volume"`
}

func mapBackpackInterval(interval exchange.Interval) string {
	if interval == exchange.Interval1M {
		return "1month"
	}
	return string(interval)
}

// FetchKlines fetches public K-lines for backpack.
//
//nolint:goconst // JSON payload keys
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", mapBackpackInterval(interval))
	params.Set("limit", "100")

	var startTimeSec int64
	if !start.IsZero() {
		startTimeSec = start.Unix()
	} else {
		// Backpack requires startTime, default to 2 hours ago if empty
		startTimeSec = time.Now().Add(-2 * time.Hour).Unix()
	}
	params.Set("startTime", strconv.FormatInt(startTimeSec, 10))

	if !end.IsZero() {
		params.Set("endTime", strconv.FormatInt(end.Unix(), 10))
	}

	path := "/klines?" + params.Encode()
	body, err := c.request(ctx, http.MethodGet, path)
	if err != nil {
		return nil, fmt.Errorf("backpack fetch klines: %w", err)
	}

	var rawKlines []backpackCandle
	if err := json.Unmarshal(body, &rawKlines); err != nil {
		return nil, fmt.Errorf("backpack unmarshal klines: %w", err)
	}

	klines := make([]exchange.Kline, 0, len(rawKlines))
	for i := range rawKlines {
		item := &rawKlines[i]
		parsedTime, err := time.Parse("2006-01-02 15:04:05", item.Start)
		if err != nil {
			continue
		}

		open, _ := strconv.ParseFloat(item.Open, 64)
		high, _ := strconv.ParseFloat(item.High, 64)
		low, _ := strconv.ParseFloat(item.Low, 64)
		closeVal, _ := strconv.ParseFloat(item.Close, 64)
		volume, _ := strconv.ParseFloat(item.Volume, 64)

		klines = append(klines, exchange.Kline{
			Timestamp: parsedTime.UnixMilli(),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeVal,
			Volume:    volume,
		})
	}

	return klines, nil
}
