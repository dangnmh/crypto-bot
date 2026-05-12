package mexc

import (
	"context"
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	body, err := c.GetCtx(ctx, "/api/v1/private/account/assets", nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]exchange.AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse assets response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get assets failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	path := fmt.Sprintf("/api/v1/private/account/asset/%s", currency)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[exchange.AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse asset response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get asset failed [%d]: %s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]string{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/position/open_positions", params)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]exchange.Position]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse positions response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get positions failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}
