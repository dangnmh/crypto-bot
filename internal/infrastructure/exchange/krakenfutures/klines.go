package krakenfutures

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// FetchKlines fetches public K-lines for krakenfutures.
func (c *Client) FetchKlines(ctx context.Context, symbol string, _ exchange.Interval, start, end time.Time) ([]exchange.Kline, error) {
	return nil, fmt.Errorf("krakenfutures does not support FetchKlines")
}
