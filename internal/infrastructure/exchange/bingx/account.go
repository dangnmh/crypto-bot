package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"crypto-bot/internal/infrastructure/exchange"
)

type bingxBalance struct {
	Asset           string      `json:"asset"`
	Balance         interface{} `json:"balance"`
	Equity          interface{} `json:"equity"`
	AvailableMargin interface{} `json:"availableMargin"`
}

type bingxPosition struct {
	Symbol           string      `json:"symbol"`
	PositionSide     string      `json:"positionSide"`
	PositionAmt      interface{} `json:"positionAmt"`
	EntryPrice       interface{} `json:"entryPrice"`
	UnrealizedProfit interface{} `json:"unrealizedProfit"`
	Leverage         interface{} `json:"leverage"`
	Isolated         bool        `json:"isolated"`
}

// GetAssets fetches all active margin balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	type balanceData struct {
		Balance bingxBalance `json:"balance"`
	}

	var res []bingxBalance
	if err := json.Unmarshal(body, &res); err != nil {
		parsed, err := ParseResponse[[]bingxBalance](body, "get_assets")
		if err == nil {
			res = parsed
		} else {
			single, err := ParseResponse[balanceData](body, "get_assets")
			if err == nil {
				res = []bingxBalance{single.Balance}
			} else {
				return nil, fmt.Errorf("parse assets response: %w", err)
			}
		}
	}

	assets := make([]exchange.AssetInfo, 0, len(res))
	for i := range res {
		b := &res[i]
		bal := parseFloat(b.Balance)
		eq := parseFloat(b.Equity)
		avail := parseFloat(b.AvailableMargin)

		assets = append(assets, exchange.AssetInfo{
			Currency:         b.Asset,
			Equity:           eq,
			AvailableBalance: avail,
			CashBalance:      bal,
		})
	}

	return assets, nil
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
	params := map[string]string{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[[]bingxPosition](body, "open_positions")
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(res))
	for i := range res {
		p := &res[i]

		amt := parseFloat(p.PositionAmt)
		if amt == 0 {
			continue
		}

		sideVal := 1 // long
		if p.PositionSide == posSideShort || (p.PositionSide == posSideBoth && amt < 0) {
			sideVal = 2 // short
		}

		absAmt := math.Abs(amt)
		lev := int(parseInt64(p.Leverage))
		entry := parseFloat(p.EntryPrice)
		pnl := parseFloat(p.UnrealizedProfit)

		marginMode := 1 // isolated
		if !p.Isolated {
			marginMode = 2 // cross
		}

		positions = append(positions, exchange.Position{
			Symbol:       p.Symbol,
			HoldVol:      absAmt,
			Leverage:     lev,
			HoldAvgPrice: entry,
			PositionType: sideVal,
			OpenType:     marginMode,
			Realised:     pnl,
		})
	}

	return positions, nil
}
