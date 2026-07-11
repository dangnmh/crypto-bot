package koinbay

import (
	"context"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// FetchKlines fetches public K-lines for koinbay.
func (c *Client) FetchKlines(ctx context.Context, symbol string, interval exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	return nil, nil
}
