package toobit

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type toobitPositionsRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type toobitPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	AvgPrice      string `json:"avgPrice"`
	Position      string `json:"position"`
	UnrealizedPnl string `json:"unrealizedPnl"`
	Leverage      string `json:"leverage"`
}

type toobitHistoryPosition struct {
	Symbol                string       `json:"symbol"`
	Side                  string       `json:"side"`
	Position              string       `json:"position"`
	OpenValue             string       `json:"openValue"`
	CloseValue            string       `json:"closeValue"`
	CloseTotalQty         string       `json:"closeTotalQty"`
	RealizedPnL           string       `json:"realizedPnL"`
	RealizedPnlRate       string       `json:"realizedPnlRate"`
	RealizedPnlWithoutFee string       `json:"realizedPnlWithoutFee"`
	Status                string       `json:"status"`
	OpenAvgPrice          string       `json:"openAvgPrice"`
	CloseAvgPrice         string       `json:"closeAvgPrice"`
	OpenFee               string       `json:"openFee"`
	CloseFee              string       `json:"closeFee"`
	OpenTime              xjson.Number `json:"openTime"`
	CloseTime             xjson.Number `json:"closeTime"`
	ID                    string       `json:"id"`
}

type toobitFuturesBalanceFlowRow struct {
	ID            xjson.Number `json:"id"`
	Coin          string       `json:"coin"`
	FlowTypeValue int          `json:"flowTypeValue"`
	FlowType      string       `json:"flowType"`
	FlowName      string       `json:"flowName"`
	Change        string       `json:"change"`
	Total         string       `json:"total"`
	Created       xjson.Number `json:"created"`
}

// Private raw methods.

func (c *Client) rawGetOpenPositions(ctx context.Context, req toobitPositionsRequest) ([]toobitPosition, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[symbolKey] = req.Symbol
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/positions", params, true)
	if err != nil {
		return nil, fmt.Errorf("toobit get raw open positions: %w", err)
	}
	data, err := parseResponse[[]toobitPosition](body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

func (c *Client) rawGetHistoryPositions(ctx context.Context, params map[string]string) ([]toobitHistoryPosition, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/historyPositions", params, true)
	if err != nil {
		return nil, fmt.Errorf("query closed position history failed: %w", err)
	}
	histData, err := parseResponse[[]toobitHistoryPosition](body)
	if err != nil {
		return nil, fmt.Errorf("parse history positions: %w", err)
	}
	return histData, nil
}

func (c *Client) rawGetBalanceFlow(ctx context.Context, params map[string]string) ([]toobitFuturesBalanceFlowRow, error) {
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/balanceFlow", params, true)
	if err != nil {
		return nil, err
	}
	rows, err := parseResponse[[]toobitFuturesBalanceFlowRow](body)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Public mapper methods.

// GetOpenPositions returns all open futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	data, err := c.rawGetOpenPositions(ctx, toobitPositionsRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	var positions []exchange.Position
	for i := range data {
		raw := &data[i]
		if symbol != "" && raw.Symbol != symbol {
			continue
		}
		vol := decmath.ParseFloat(raw.Position)
		if vol <= 0 {
			continue
		}

		pType := exchange.PositionTypeLong
		if raw.Side == posSideShort {
			pType = exchange.PositionTypeShort
		}

		avgPrice := decmath.ParseFloat(raw.AvgPrice)
		pnl := decmath.ParseFloat(raw.UnrealizedPnl)
		levVal, _ := strconv.Atoi(raw.Leverage)

		positions = append(positions, exchange.Position{
			Symbol:          raw.Symbol,
			HoldVol:         vol,
			PositionType:    pType,
			OpenAvgPrice:    avgPrice,
			HoldAvgPrice:    avgPrice,
			CloseProfitLoss: pnl,
			Leverage:        levVal,
		})
	}
	return positions, nil
}

// ClosePosition closes a position by submitting a market reduction order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	submitSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		submitSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Side:         submitSide,
		Type:         exchange.OrderTypeMarket,
		Vol:          volume,
		PositionMode: positionMode,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "toobit"),
		Leverage:     leverage,
	})
	return err
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := &positions[i]
		closeSide := domain.SideCloseLong
		if pos.PositionType == exchange.PositionTypeShort {
			closeSide = domain.SideCloseShort
		}
		err = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1, pos.Leverage)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetOrderPNL queries the historical closed position metrics from Toobit.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("toobit get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == domain.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	var positionSide string
	if orderInfo.Side == exchange.SideOpenLong || orderInfo.Side == exchange.SideCloseLong {
		positionSide = posSideLong
	} else {
		positionSide = posSideShort
	}

	params := map[string]string{
		symbolKey: symbol,
		"side":    positionSide,
	}

	if orderInfo.CreateTime > 0 {
		params["startTime"] = strconv.FormatInt(orderInfo.CreateTime, 10)
	}

	histData, err := c.rawGetHistoryPositions(ctx, params)
	if err != nil {
		return nil, err
	}

	if len(histData) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed position history found for symbol %s", symbol)
	}

	row := histData[0]

	entryPrice, _ := strconv.ParseFloat(row.OpenAvgPrice, 64)
	exitPrice, _ := strconv.ParseFloat(row.CloseAvgPrice, 64)
	closedSize, _ := strconv.ParseFloat(row.CloseTotalQty, 64)
	grossPnL, _ := strconv.ParseFloat(row.RealizedPnlWithoutFee, 64)
	netPnL, _ := strconv.ParseFloat(row.RealizedPnL, 64)
	openFee, _ := strconv.ParseFloat(row.OpenFee, 64)
	closeFee, _ := strconv.ParseFloat(row.CloseFee, 64)

	duration := max(xjson.ToInt64(row.CloseTime)-xjson.ToInt64(row.OpenTime), 0)

	pnlRate := 0.0
	if entryPrice > 0 {
		if positionSide == posSideLong {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	fundingFee, err := c.getHoldFee(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		c.logger.Debug("failed to fetch funding fee", "error", err)
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        math.Abs(openFee) + math.Abs(closeFee),
		FundingFee: fundingFee,
		DurationMs: duration,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
	}, nil
}

// getHoldFee queries the futures balance flow log to find funding fee payments for a symbol.
func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime int64) (float64, error) {
	if startTime == 0 {
		return 0, nil
	}

	params := map[string]string{
		symbolKey:   symbol,
		"startTime": strconv.FormatInt(startTime, 10),
		"limit":     "1000",
	}

	rows, err := c.rawGetBalanceFlow(ctx, params)
	if err != nil {
		return 0, err
	}

	totalFunding := 0.0
	for _, row := range rows {
		if row.FlowTypeValue == 32 {
			val, _ := strconv.ParseFloat(row.Change, 64)
			totalFunding += val
		}
	}

	return totalFunding, nil
}
