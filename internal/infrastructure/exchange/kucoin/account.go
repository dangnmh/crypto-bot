package kucoin

import (
	"context"
	"math"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type kucoinAccountOverview struct {
	Currency         string `json:"currency"`
	AccountEquity    string `json:"accountEquity"`
	AvailableBalance string `json:"availableBalance"`
	PositionMargin   string `json:"positionMargin"`
	OrderMargin      string `json:"orderMargin"`
	UnrealisedPNL    string `json:"unrealisedPNL"`
}

type kucoinPosition struct {
	Symbol           string `json:"symbol"`
	CurrentQty       string `json:"currentQty"`
	AvgEntryPrice    string `json:"avgEntryPrice"`
	RealisedPNL      string `json:"realisedPNL"`
	UnrealisedPNL    string `json:"unrealisedPNL"`
	Leverage         string `json:"leverage"`
	LiquidationPrice string `json:"liquidationPrice"`
}

// GetAssets fetches account balance overview.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	overview, err := ParseResponse[kucoinAccountOverview](body, "account_balance")
	if err != nil {
		return nil, err
	}

	eq := decmath.ParseFloat(overview.AccountEquity)
	avail := decmath.ParseFloat(overview.AvailableBalance)
	upl := decmath.ParseFloat(overview.UnrealisedPNL)

	return []exchange.AssetInfo{
		{
			Currency:         overview.Currency,
			Equity:           eq,
			AvailableBalance: avail,
			CashBalance:      eq,
			Unrealized:       upl,
		},
	}, nil
}

// GetAssetByCurrency retrieves margin balance for a specific coin.
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

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions retrieves currently active futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	body, err := c.GetCtx(ctx, pathOpenPositions, nil)
	if err != nil {
		return nil, err
	}

	positions, err := ParseResponse[[]kucoinPosition](body, "open_positions")
	if err != nil {
		return nil, err
	}

	openPositions := make([]exchange.Position, 0, len(positions))
	for i := range positions {
		p := &positions[i]
		if symbol != "" && p.Symbol != symbol {
			continue
		}

		amt := decmath.ParseFloat(p.CurrentQty)
		if amt == 0 {
			continue
		}

		posType := 1 // long
		if amt < 0 {
			posType = 2 // short
		}

		absAmt := math.Abs(amt)
		lever := int(decmath.ParseInt64(p.Leverage))
		avgPx := decmath.ParseFloat(p.AvgEntryPrice)
		liqPx := decmath.ParseFloat(p.LiquidationPrice)
		realized := decmath.ParseFloat(p.RealisedPNL)

		openPositions = append(openPositions, exchange.Position{
			Symbol:         p.Symbol,
			HoldVol:        absAmt,
			Leverage:       lever,
			HoldAvgPrice:   avgPx,
			LiquidatePrice: liqPx,
			Realised:       realized,
			PositionType:   posType,
			OpenType:       2, // cross margin is default
		})
	}

	return openPositions, nil
}
