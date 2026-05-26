package hyperliquid

import (
	"context"
	"fmt"
	"math"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
)

// GetAssets retrieves account balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	state, err := c.info.UserState(ctx, c.userAddress)
	if err != nil {
		return nil, err
	}

	equity, _ := strconv.ParseFloat(state.MarginSummary.AccountValue, 64)
	avail, _ := strconv.ParseFloat(state.Withdrawable, 64)
	marginUsed, _ := strconv.ParseFloat(state.MarginSummary.TotalMarginUsed, 64)

	assets := []exchange.AssetInfo{
		{
			Currency:         settleUsdc,
			Equity:           equity,
			AvailableBalance: avail,
			FrozenBalance:    marginUsed,
			CashBalance:      equity - marginUsed,
			Unrealized:       equity - (avail + marginUsed),
		},
	}
	return assets, nil
}

// GetAssetByCurrency retrieves balance details for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		if assets[i].Currency == currency {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("asset not found: %s", currency)
}

// GetOpenPositions returns all currently open perp positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	state, err := c.info.UserState(ctx, c.userAddress)
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(state.AssetPositions))
	for i := range state.AssetPositions {
		p := &state.AssetPositions[i].Position
		if symbol != "" && p.Coin != symbol {
			continue
		}

		szi, _ := strconv.ParseFloat(p.Szi, 64)
		if szi == 0 {
			continue
		}

		entryPrice := 0.0
		if p.EntryPx != nil {
			entryPrice, _ = strconv.ParseFloat(*p.EntryPx, 64)
		}

		liqPrice := 0.0
		if p.LiquidationPx != nil {
			liqPrice, _ = strconv.ParseFloat(*p.LiquidationPx, 64)
		}

		unrealized, _ := strconv.ParseFloat(p.UnrealizedPnl, 64)

		posSide := 1 // Long
		if szi < 0 {
			posSide = 2 // Short
		}

		positions = append(positions, exchange.Position{
			Symbol:         p.Coin,
			HoldVol:        math.Abs(szi),
			HoldAvgPrice:   entryPrice,
			OpenAvgPrice:   entryPrice,
			LiquidatePrice: liqPrice,
			Realised:       unrealized,
			Leverage:       p.Leverage.Value,
			PositionType:   posSide,
		})
	}
	return positions, nil
}
