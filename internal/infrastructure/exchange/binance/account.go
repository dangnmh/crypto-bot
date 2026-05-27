package binance

import (
	"context"
	"fmt"
	"math"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/samber/lo"
)

// GetAssets returns all account assets and balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	req := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2(ctx)
	resp, err := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2Execute(req)
	if err != nil {
		return nil, fmt.Errorf("binance futures account balance: %w", err)
	}

	items := resp.Data.Items
	assets := make([]exchange.AssetInfo, 0, len(items))

	for _, item := range items {
		asset := item.GetAsset()
		balance := decmath.ParseFloat(lo.FromPtr(item.Balance))
		crossUnPnl := decmath.ParseFloat(lo.FromPtr(item.CrossUnPnl))
		available := decmath.ParseFloat(lo.FromPtr(item.AvailableBalance))

		assets = append(assets, exchange.AssetInfo{
			Currency:         asset,
			PositionMargin:   0.0,
			FrozenBalance:    0.0,
			AvailableBalance: available,
			CashBalance:      balance,
			Equity:           balance + crossUnPnl,
			Unrealized:       crossUnPnl,
		})
	}

	if len(assets) == 0 {
		assets = append(assets, exchange.AssetInfo{
			Currency: "USDT",
		})
	}

	return assets, nil
}

// GetAssetByCurrency queries asset information for a single currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for i := range assets {
		if strings.EqualFold(assets[i].Currency, currency) {
			return &assets[i], nil
		}
	}

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions returns all open positions for a symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	req := c.sdkClient.RestApi.TradeAPI.PositionInformationV2(ctx)
	if symbol != "" {
		req = req.Symbol(symbol)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.PositionInformationV2Execute(req)
	if err != nil {
		return nil, fmt.Errorf("binance position information: %w", err)
	}

	items := resp.Data.Items
	positions := make([]exchange.Position, 0, len(items))

	for i := range items {
		raw := items[i]
		amt := decmath.ParseFloat(lo.FromPtr(raw.PositionAmt))

		if math.Abs(amt) == 0 {
			continue
		}

		entryPrice := decmath.ParseFloat(lo.FromPtr(raw.EntryPrice))
		liqPrice := decmath.ParseFloat(lo.FromPtr(raw.LiquidationPrice))
		unrealized := decmath.ParseFloat(lo.FromPtr(raw.UnRealizedProfit))
		lev := decmath.ParseInt(lo.FromPtrOr(raw.Leverage, "1"))

		posType := 1
		if amt < 0 {
			posType = 2
		}

		openType := 1
		if strings.EqualFold(raw.GetMarginType(), "cross") {
			openType = 2
		}

		if raw.GetPositionSide() == posSideShort {
			posType = 2
		}

		positions = append(positions, exchange.Position{
			Symbol:         raw.GetSymbol(),
			HoldVol:        math.Abs(amt),
			HoldAvgPrice:   entryPrice,
			OpenAvgPrice:   entryPrice,
			LiquidatePrice: liqPrice,
			Realised:       unrealized,
			Leverage:       lev,
			PositionType:   posType,
			OpenType:       openType,
		})
	}

	return positions, nil
}
