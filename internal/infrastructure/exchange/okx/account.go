package okx

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
)

type okxBalanceDetail struct {
	Ccy       string `json:"ccy"`
	Eq        string `json:"eq"`
	AvailBal  string `json:"availBal"`
	FrozenBal string `json:"frozenBal"`
	Upl       string `json:"upl"`
}

type okxBalance struct {
	Details []okxBalanceDetail `json:"details"`
}

type okxPosition struct {
	InstID      string `json:"instId"`
	Pos         string `json:"pos"`
	Lever       string `json:"lever"`
	AvgPx       string `json:"avgPx"`
	LiqPx       string `json:"liqPx"`
	RealizedPnl string `json:"realizedPnl"`
	Margin      string `json:"margin"`
	PosSide     string `json:"posSide"`
	MgnMode     string `json:"mgnMode"`
}

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	balances, err := ParseResponse[okxBalance](body, "assets")
	if err != nil {
		return nil, err
	}

	if len(balances) == 0 {
		return nil, fmt.Errorf("empty balance response")
	}

	var assets []exchange.AssetInfo
	detailsList := balances[0].Details
	for i := range detailsList {
		details := detailsList[i]
		eq, _ := strconv.ParseFloat(details.Eq, 64)
		avail, _ := strconv.ParseFloat(details.AvailBal, 64)
		frozen, _ := strconv.ParseFloat(details.FrozenBal, 64)
		upl, _ := strconv.ParseFloat(details.Upl, 64)

		assets = append(assets, exchange.AssetInfo{
			Currency:         details.Ccy,
			Equity:           eq,
			AvailableBalance: avail,
			FrozenBalance:    frozen,
			CashBalance:      eq,
			Unrealized:       upl,
		})
	}

	return assets, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	params := map[string]string{
		"ccy": currency,
	}

	body, err := c.GetCtx(ctx, pathAccountBalance, params)
	if err != nil {
		return nil, err
	}

	balances, err := ParseResponse[okxBalance](body, "asset_by_currency")
	if err != nil {
		return nil, err
	}

	if len(balances) == 0 || len(balances[0].Details) == 0 {
		return nil, fmt.Errorf("asset balance not found for currency: %s", currency)
	}

	details := balances[0].Details[0]
	eq, _ := strconv.ParseFloat(details.Eq, 64)
	avail, _ := strconv.ParseFloat(details.AvailBal, 64)
	frozen, _ := strconv.ParseFloat(details.FrozenBal, 64)
	upl, _ := strconv.ParseFloat(details.Upl, 64)

	return &exchange.AssetInfo{
		Currency:         details.Ccy,
		Equity:           eq,
		AvailableBalance: avail,
		FrozenBalance:    frozen,
		CashBalance:      eq,
		Unrealized:       upl,
	}, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]string{
		"instType": "SWAP",
	}
	if symbol != "" {
		params["instId"] = symbol
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	positions, err := ParseResponse[okxPosition](body, "open_positions")
	if err != nil {
		return nil, err
	}

	var openPositions []exchange.Position
	for i := range positions {
		pos := positions[i]
		holdVol, _ := strconv.ParseFloat(pos.Pos, 64)
		if holdVol <= 0 {
			continue
		}

		lever, _ := strconv.Atoi(pos.Lever)
		avgPx, _ := strconv.ParseFloat(pos.AvgPx, 64)
		liqPx, _ := strconv.ParseFloat(pos.LiqPx, 64)
		realized, _ := strconv.ParseFloat(pos.RealizedPnl, 64)
		margin, _ := strconv.ParseFloat(pos.Margin, 64)

		posType := 1 // long
		if pos.PosSide == posSideShort {
			posType = 2
		}

		openType := 1 // isolated
		if pos.MgnMode == modeCross {
			openType = 2
		}

		openPositions = append(openPositions, exchange.Position{
			Symbol:         pos.InstID,
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
