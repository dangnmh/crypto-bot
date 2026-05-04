package exchange

import (
	"context"
	"encoding/json"
	"fmt"
)

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]AssetInfo, error) {
	body, err := c.GetCtx(ctx, "/api/v1/private/account/assets", nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse assets response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get assets failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*AssetInfo, error) {
	path := fmt.Sprintf("/api/v1/private/account/asset/%s", currency)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse asset response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get asset failed [%d]: %s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]Position, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/position/open_positions", params)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]Position]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse positions response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get positions failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}
