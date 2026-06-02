package kucoin

import (
	"context"
	"math"
	"time"

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

type kucoinAssetsRequest struct{}

type kucoinOpenPositionsRequest struct{}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) getRawAssets(ctx context.Context, _ kucoinAssetsRequest) (*kucoinAccountOverview, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	overview, err := ParseResponse[kucoinAccountOverview](body, "account_balance")
	if err != nil {
		return nil, err
	}
	return &overview, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, _ kucoinOpenPositionsRequest) ([]kucoinPosition, error) {
	body, err := c.GetCtx(ctx, pathOpenPositions, nil)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]kucoinPosition](body, "open_positions")
}

// Public mapper methods implementing the exchange.AccountDataProvider interface.

// GetAssets fetches account balance overview.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	overview, err := c.getRawAssets(ctx, kucoinAssetsRequest{})
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
	positions, err := c.getRawOpenPositions(ctx, kucoinOpenPositionsRequest{})
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

		posType := 1 // Long.
		if amt < 0 {
			posType = 2 // Short.
		}

		absAmt := math.Abs(amt)
		avgPx := decmath.ParseFloat(p.AvgEntryPrice)

		openPositions = append(openPositions, exchange.Position{
			Symbol:       p.Symbol,
			HoldVol:      absAmt,
			HoldAvgPrice: avgPx,
			OpenAvgPrice: avgPx,
			PositionType: posType,
		})
	}

	return openPositions, nil
}

// GetRecentClosedPnL is not supported on Kucoin client yet.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	return nil, exchange.ErrNotSupported
}
