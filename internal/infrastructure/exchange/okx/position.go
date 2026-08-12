package okx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

const exchangeName = "okx"

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

func (c *Client) rawGetOpenPositions(ctx context.Context, req okxPositionsRequest) ([]okxPosition, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	body, err := c.GetOpenPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxPosition](body, "open_positions")
}

func (c *Client) rawGetClosedPositions(ctx context.Context, req okxClosedPositionsRequest) ([]okxClosedPosition, error) {
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
	body, err := c.GetClosedPnLRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxClosedPosition](body, "positions_history")
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	positions, err := c.rawGetOpenPositions(ctx, okxPositionsRequest{
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
		lev, _ := strconv.Atoi(pos.Lever)

		posType := mapPositionType(pos.PosSide, posVal, pos.InstID, pos.PosCcy)

		openPositions = append(openPositions, exchange.Position{
			Symbol:          pos.InstID,
			HoldVolContract: holdVol,
			RawHoldVol:      holdVol,
			HoldAvgPrice:    avgPx,
			OpenAvgPrice:    avgPx,
			PositionType:    posType,
			Leverage:        lev,
		})
	}

	return openPositions, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "okx"),
		Leverage:     leverage,
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := positions[i]
		vol := pos.HoldVolContract
		if vol == 0 {
			vol = pos.HoldVolCoin
		}
		if vol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == exchange.PositionTypeLong { // Long
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, vol, domain.PositionModeHedge, pos.Leverage)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// GetOrderPNL queries the historical closed position metrics from OKX.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("okx get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
			Status:   orderInfo.State,
		}, nil
	}

	req := okxClosedPositionsRequest{
		InstType: instTypeSwap,
		Limit:    "10",
		InstID:   symbol,
	}
	if orderInfo.CreateTime > 0 {
		req.Begin = strconv.FormatInt(orderInfo.CreateTime-1000, 10)
	}

	positions, err := c.rawGetClosedPositions(ctx, req)
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
		Exchange:           exchangeName,
		Symbol:             pos.InstID,
		Status:             orderInfo.State,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosedSizeContract: new(closedSize),
		GrossPnL:           closedPnl,
		Fee:                math.Abs(feeVal),
		FundingFee:         fundingFeeVal,
		DurationMs:         duration,
		NetPnl:             netPnlVal,
		PnLRate:            pnlRate,
	}, nil
}
