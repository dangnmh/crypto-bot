package okx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

// Explicit request/response structs for account endpoints.

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

type okxBalanceRequest struct {
	Ccy string `json:"ccy,omitempty"`
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
	PosCcy      string `json:"posCcy"`
	Ccy         string `json:"ccy"`
}

type okxPositionsRequest struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId,omitempty"`
}

type okxClosedPosition struct {
	InstID        string `json:"instId"`
	CloseAvgPx    string `json:"closeAvgPx"`
	OpenAvgPx     string `json:"openAvgPx"`
	Pnl           string `json:"pnl"`
	CloseTotalPos string `json:"closeTotalPos"`
	CTime         string `json:"cTime"`
	UTime         string `json:"uTime"`
	Fee           string `json:"fee"`
	FundingFee    string `json:"fundingFee"`
	RealizedPnl   string `json:"realizedPnl"`
	PosSide       string `json:"posSide"`
	Direction     string `json:"direction"`
	Pos           string `json:"pos"`
}

type okxClosedPositionsRequest struct {
	InstType string `json:"instType"`
	Limit    string `json:"limit,omitempty"`
	InstID   string `json:"instId,omitempty"`
	Begin    string `json:"begin,omitempty"`
}

// Private raw methods invoking the OKX V5 REST API.

func (c *Client) getRawBalance(ctx context.Context, req okxBalanceRequest) ([]okxBalance, error) {
	params := map[string]string{}
	if req.Ccy != "" {
		params["ccy"] = req.Ccy
	}
	body, err := c.GetCtx(ctx, pathAccountBalance, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxBalance](body, "account_balance")
}

func (c *Client) getRawOpenPositions(ctx context.Context, req okxPositionsRequest) ([]okxPosition, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxPosition](body, "open_positions")
}

func (c *Client) getRawClosedPositions(ctx context.Context, req okxClosedPositionsRequest) ([]okxClosedPosition, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.Limit != "" {
		params[paramLimit] = req.Limit
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	if req.Begin != "" {
		params["before"] = req.Begin
	}
	body, err := c.GetCtx(ctx, "/api/v5/account/positions-history", params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxClosedPosition](body, "positions_history")
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	balances, err := c.getRawBalance(ctx, okxBalanceRequest{})
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
	balances, err := c.getRawBalance(ctx, okxBalanceRequest{Ccy: currency})
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
	positions, err := c.getRawOpenPositions(ctx, okxPositionsRequest{
		InstType: instTypeSwap,
		InstID:   symbol,
	})
	if err != nil {
		return nil, err
	}

	var openPositions []exchange.Position
	for i := range positions {
		pos := positions[i]
		posVal, _ := strconv.ParseFloat(pos.Pos, 64)
		if posVal == 0 {
			continue
		}

		holdVol := math.Abs(posVal)
		avgPx, _ := strconv.ParseFloat(pos.AvgPx, 64)

		posType := mapPositionType(pos.PosSide, posVal, pos.InstID, pos.PosCcy)

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

// GetRecentClosedPnL queries the historical closed position metrics from OKX.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	req := okxClosedPositionsRequest{
		InstType: instTypeSwap,
		Limit:    "10",
		InstID:   symbol,
	}
	if !startTime.IsZero() {
		req.Begin = strconv.FormatInt(startTime.UnixMilli(), 10)
	}

	positions, err := c.getRawClosedPositions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	if len(positions) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed position history found for symbol %s", symbol)
	}

	pos := positions[0]
	entryPrice, _ := strconv.ParseFloat(pos.OpenAvgPx, 64)
	exitPrice, _ := strconv.ParseFloat(pos.CloseAvgPx, 64)
	closedSize, _ := strconv.ParseFloat(pos.CloseTotalPos, 64)
	closedPnl, _ := strconv.ParseFloat(pos.Pnl, 64)
	feeVal, _ := strconv.ParseFloat(pos.Fee, 64)
	fundingFeeVal, _ := strconv.ParseFloat(pos.FundingFee, 64)
	netPnlVal, _ := strconv.ParseFloat(pos.RealizedPnl, 64)

	cTime, _ := strconv.ParseInt(pos.CTime, 10, 64)
	uTime, _ := strconv.ParseInt(pos.UTime, 10, 64)
	duration := max(uTime-cTime, 0)

	posVal, _ := strconv.ParseFloat(pos.Pos, 64)
	side := mapPositionType(pos.PosSide, posVal, pos.InstID, "")
	switch pos.Direction {
	case "short":
		side = 2
	case "long":
		side = 1
	}

	pnlRate := 0.0
	if entryPrice > 0 {
		if side == 2 { // short
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		} else { // long/default
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		}
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     pos.InstID,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   closedPnl,
		Fee:        math.Abs(feeVal),
		FundingFee: fundingFeeVal,
		DurationMs: duration,
		NetPnl:     netPnlVal,
		PnLRate:    pnlRate,
	}, nil
}
