package gate

import (
	"context"
	"fmt"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/gateio/gateapi-go/v7"
)

// GetAssets returns the account assets.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	ctx = c.authCtx(ctx)
	resp, httpResp, err := c.apiClient.FuturesApi.ListFuturesAccounts(ctx, "usdt")
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list assets: %w", err)
	}

	equity := decmath.ParseFloat(resp.Total)
	unrealized := decmath.ParseFloat(resp.UnrealisedPnl)
	asset := exchange.AssetInfo{
		Currency:         resp.Currency,
		PositionMargin:   decmath.ParseFloat(resp.PositionMargin),
		AvailableBalance: decmath.ParseFloat(resp.Available),
		Equity:           equity,
		Unrealized:       unrealized,
		CashBalance:      equity - unrealized,
	}
	return []exchange.AssetInfo{asset}, nil
}

// GetAssetByCurrency returns the account asset for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for _, asset := range assets {
		if strings.EqualFold(asset.Currency, currency) {
			return &asset, nil
		}
	}

	// Fallback to zero value if not found
	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// mapPosition maps a gateapi.Position to exchange.Position.
func mapPosition(raw gateapi.Position) exchange.Position {
	pos := exchange.Position{
		Symbol:         raw.Contract,
		HoldVol:        float64(decmath.AbsInt64(raw.Size)),
		HoldAvgPrice:   decmath.ParseFloat(raw.EntryPrice),
		OpenAvgPrice:   decmath.ParseFloat(raw.EntryPrice),
		LiquidatePrice: decmath.ParseFloat(raw.LiqPrice),
		Realised:       decmath.ParseFloat(raw.RealisedPnl),
		Leverage:       decmath.ParseInt(raw.Leverage),
	}

	if raw.Size > 0 {
		pos.PositionType = 1 // Long
	} else if raw.Size < 0 {
		pos.PositionType = 2 // Short
	}

	return pos
}

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	ctx = c.authCtx(ctx)

	// If symbol is specified, we can retrieve the specific position.
	if symbol != "" {
		pos, httpResp, err := c.apiClient.FuturesApi.GetPosition(ctx, "usdt", symbol)
		if httpResp != nil && httpResp.Body != nil {
			defer func() { _ = httpResp.Body.Close() }()
		}
		if err != nil {
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
				return nil, nil
			}
			return nil, fmt.Errorf("gate.io get position for %s: %w", symbol, err)
		}

		if pos.Size == 0 {
			return nil, nil
		}
		return []exchange.Position{mapPosition(pos)}, nil
	}

	// Retrieve all positions
	rawPositions, httpResp, err := c.apiClient.FuturesApi.ListPositions(ctx, "usdt", nil)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, fmt.Errorf("gate.io list positions: %w", err)
	}

	positions := make([]exchange.Position, 0, len(rawPositions))
	for i := range rawPositions {
		pos := &rawPositions[i]
		if pos.Size != 0 {
			positions = append(positions, mapPosition(*pos))
		}
	}
	return positions, nil
}
