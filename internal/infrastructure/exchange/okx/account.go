package okx

import (
	"context"
	"fmt"
	"strconv"
	"time"

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

func (c *Client) getRawOpenPositions(ctx context.Context, symbol string) ([]okxPosition, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	if symbol != "" {
		params[paramInstId] = symbol
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[okxPosition](body, "open_positions")
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	positions, err := c.getRawOpenPositions(ctx, symbol)
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

		avgPx, _ := strconv.ParseFloat(pos.AvgPx, 64)

		posType := 1 // long
		if pos.PosSide == posSideShort {
			posType = 2
		}

		openPositions = append(openPositions, exchange.Position{
			Symbol:       pos.InstID,
			HoldVol:      holdVol,
			HoldAvgPrice: avgPx,
			OpenAvgPrice: avgPx,
			PositionType: posType,
		})
	}

	return openPositions, nil
}

type okxClosedPosition struct {
	InstID       string `json:"instId"`
	CloseAvgPx   string `json:"closeAvgPx"`
	OpenAvgPx    string `json:"openAvgPx"`
	Pnl          string `json:"pnl"`
	CloseTotalSz string `json:"closeTotalSz"`
	CTime        string `json:"cTime"`
	UTime        string `json:"uTime"`
}

// GetRecentClosedPnL queries the historical closed position metrics from OKX.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
		paramLimit:    "10",
	}
	if symbol != "" {
		params[paramInstId] = symbol
	}
	if !startTime.IsZero() {
		params["begin"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	}

	body, err := c.GetCtx(ctx, "/api/v5/account/positions-history", params)
	if err != nil {
		return nil, err
	}

	positions, err := ParseResponse[okxClosedPosition](body, "positions_history")
	if err != nil {
		return nil, err
	}

	if len(positions) == 0 {
		return nil, fmt.Errorf("no closed position history found for symbol %s", symbol)
	}

	pos := positions[0]
	entryPrice, _ := strconv.ParseFloat(pos.OpenAvgPx, 64)
	exitPrice, _ := strconv.ParseFloat(pos.CloseAvgPx, 64)
	closedSize, _ := strconv.ParseFloat(pos.CloseTotalSz, 64)
	closedPnl, _ := strconv.ParseFloat(pos.Pnl, 64)

	cTime, _ := strconv.ParseInt(pos.CTime, 10, 64)
	uTime, _ := strconv.ParseInt(pos.UTime, 10, 64)
	duration := max(uTime-cTime, 0)

	return &exchange.ClosedPnLInfo{
		Symbol:     pos.InstID,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   closedPnl,
		Fee:        0,
		FundingFee: 0,
		DurationMs: duration,
	}, nil
}
