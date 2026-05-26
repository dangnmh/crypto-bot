package bitget

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
)

type bitgetAccountAsset struct {
	MarginCoin    string `json:"marginCoin"`
	Locked        string `json:"locked"`
	Available     string `json:"available"`
	AccountEquity string `json:"accountEquity"`
	UnrealizedPL  string `json:"unrealizedPL"`
}

type bitgetPosition struct {
	Symbol           string `json:"symbol"`
	HoldSide         string `json:"holdSide"`
	MarginMode       string `json:"marginMode"`
	Leverage         string `json:"leverage"`
	Total            string `json:"total"`
	Available        string `json:"available"`
	Locked           string `json:"locked"`
	OpenPriceAvg     string `json:"openPriceAvg"`
	MarginSize       string `json:"marginSize"`
	UnrealizedPL     string `json:"unrealizedPL"`
	LiquidationPrice string `json:"liquidationPrice"`
	AchievedProfits  string `json:"achievedProfits"`
}

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathAccountBalance, params)
	if err != nil {
		return nil, err
	}

	balances, err := ParseResponse[[]bitgetAccountAsset](body, "assets")
	if err != nil {
		return nil, err
	}

	assets := make([]exchange.AssetInfo, 0, len(balances))
	for i := range balances {
		item := &balances[i]
		eq, _ := strconv.ParseFloat(item.AccountEquity, 64)
		avail, _ := strconv.ParseFloat(item.Available, 64)
		locked, _ := strconv.ParseFloat(item.Locked, 64)
		upl, _ := strconv.ParseFloat(item.UnrealizedPL, 64)

		assets = append(assets, exchange.AssetInfo{
			Currency:         item.MarginCoin,
			Equity:           eq,
			AvailableBalance: avail,
			FrozenBalance:    locked,
			CashBalance:      eq,
			Unrealized:       upl,
		})
	}

	return assets, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
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

	return nil, fmt.Errorf("asset balance not found for currency: %s", currency)
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	positions, err := ParseResponse[[]bitgetPosition](body, "open_positions")
	if err != nil {
		return nil, err
	}

	var openPositions []exchange.Position
	for i := range positions {
		pos := &positions[i]
		if symbol != "" && pos.Symbol != symbol {
			continue
		}

		holdVol, _ := strconv.ParseFloat(pos.Total, 64)
		if holdVol <= 0 {
			continue
		}

		lever, _ := strconv.Atoi(pos.Leverage)
		avgPx, _ := strconv.ParseFloat(pos.OpenPriceAvg, 64)
		liqPx, _ := strconv.ParseFloat(pos.LiquidationPrice, 64)
		realized, _ := strconv.ParseFloat(pos.AchievedProfits, 64)
		margin, _ := strconv.ParseFloat(pos.MarginSize, 64)

		posType := 1 // long
		if pos.HoldSide == posSideShort {
			posType = 2
		}

		openType := 1 // isolated
		if pos.MarginMode == modeCrossed || pos.MarginMode == modeCross {
			openType = 2
		}

		openPositions = append(openPositions, exchange.Position{
			Symbol:         pos.Symbol,
			HoldVol:        holdVol,
			Leverage:       lever,
			HoldAvgPrice:   avgPx,
			LiquidatePrice: liqPx,
			Realised:       realized,
			IM:             margin,
			PositionType:   posType,
			OpenType:       openType,
		})
	}

	return openPositions, nil
}
