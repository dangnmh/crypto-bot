package deepcoin

import (
	"context"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	return nil, fmt.Errorf("GetAssets not supported on Deepcoin")
}

func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	return nil, fmt.Errorf("GetAssetByCurrency not supported on Deepcoin")
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	return nil, fmt.Errorf("GetOpenPositions not supported on Deepcoin")
}
