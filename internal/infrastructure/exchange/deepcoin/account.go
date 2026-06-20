package deepcoin

import (
	"context"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return nil, fmt.Errorf("GetOpenPositions not supported on Deepcoin")
}
