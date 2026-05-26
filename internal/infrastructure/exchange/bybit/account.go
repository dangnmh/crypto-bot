package bybit

import (
	"context"
	"fmt"
	"strings"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bybitCoinBalance struct {
	Coin          string `json:"coin"`
	Equity        string `json:"equity"`
	WalletBalance string `json:"walletBalance"`
	UnrealisedPnl string `json:"unrealisedPnl"`
}

type bybitWalletBalance struct {
	TotalEquity        string             `json:"totalEquity"`
	TotalWalletBalance string             `json:"totalWalletBalance"`
	Coin               []bybitCoinBalance `json:"coin"`
}

type bybitWalletBalanceResult struct {
	List []bybitWalletBalance `json:"list"`
}

type bybitPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Size          string `json:"size"`
	EntryPrice    string `json:"entryPrice"`
	LiqPrice      string `json:"liqPrice"`
	Leverage      string `json:"leverage"`
	PositionIdx   int    `json:"positionIdx"`
	UnrealisedPnl string `json:"unrealisedPnl"`
}

type bybitPositionResult struct {
	Category string          `json:"category"`
	List     []bybitPosition `json:"list"`
}

// GetAssets returns the account assets.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	apiAccountType := "CONTRACT"
	if strings.EqualFold(c.accountType, "unified") {
		apiAccountType = "UNIFIED"
	}

	params := map[string]interface{}{
		"accountType": apiAccountType,
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetAccountWallet(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit list assets: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit list assets error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitWalletBalanceResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode wallet balance: %w", err)
	}

	assets := []exchange.AssetInfo{}
	for _, wallet := range res.List {
		for _, coin := range wallet.Coin {
			equity := decmath.ParseFloat(coin.Equity)
			unrealized := decmath.ParseFloat(coin.UnrealisedPnl)
			assets = append(assets, exchange.AssetInfo{
				Currency:         coin.Coin,
				AvailableBalance: decmath.ParseFloat(coin.WalletBalance) + unrealized,
				Equity:           equity,
				Unrealized:       unrealized,
				CashBalance:      equity - unrealized,
			})
		}
	}

	if len(assets) == 0 {
		// Provide default zero balance asset if empty
		assets = append(assets, exchange.AssetInfo{
			Currency: "USDT",
		})
	}

	return assets, nil
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

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// mapPosition maps a bybitPosition to exchange.Position.
func mapPosition(raw bybitPosition) exchange.Position {
	pos := exchange.Position{
		Symbol:         raw.Symbol,
		HoldVol:        decmath.ParseFloat(raw.Size),
		HoldAvgPrice:   decmath.ParseFloat(raw.EntryPrice),
		OpenAvgPrice:   decmath.ParseFloat(raw.EntryPrice),
		LiquidatePrice: decmath.ParseFloat(raw.LiqPrice),
		Realised:       decmath.ParseFloat(raw.UnrealisedPnl), // V5 contains realized inside pnl summaries
		Leverage:       decmath.ParseInt(raw.Leverage),
	}

	switch raw.PositionIdx {
	case 1:
		pos.PositionType = 1 // Long
	case 2:
		pos.PositionType = 2 // Short
	default:
		// OneWay mode fallback
		if strings.EqualFold(raw.Side, "buy") {
			pos.PositionType = 1 // Long
		} else if strings.EqualFold(raw.Side, "sell") {
			pos.PositionType = 2 // Short
		}
	}

	return pos
}

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
	}
	if symbol != "" {
		params[symbolKey] = symbol
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetPositionList(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit get position: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit get position error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitPositionResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode positions: %w", err)
	}

	positions := make([]exchange.Position, 0, len(res.List))
	for i := range res.List {
		pos := &res.List[i]
		if decmath.ParseFloat(pos.Size) > 0 {
			positions = append(positions, mapPosition(*pos))
		}
	}

	return positions, nil
}
