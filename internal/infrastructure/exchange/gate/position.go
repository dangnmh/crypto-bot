package gate

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type gatePositionsRequest struct {
	Settle string `json:"settle"`
	Symbol string `json:"symbol,omitempty"`
}

// Private raw methods.

func (c *Client) rawGetPosition(ctx context.Context, req gatePositionsRequest) ([]gatePosition, error) {
	params := map[string]string{
		paramSettle: req.Settle,
	}
	path := fmt.Sprintf("/futures/%s/dual_comp/positions/%s", req.Settle, req.Symbol)
	body, err := c.RawRequest(ctx, "GET", path, params, nil)
	if err != nil {
		return nil, err
	}
	var result []gatePosition
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) rawGetPositions(ctx context.Context, req gatePositionsRequest) ([]gatePosition, error) {
	params := map[string]string{
		paramSettle: req.Settle,
	}
	body, err := c.GetOpenPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	var result []gatePosition
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) rawGetMyTrades(ctx context.Context, settle, contract string, orderID int64) ([]gateMyTrade, error) {
	params := map[string]string{
		paramSettle: settle,
	}
	if contract != "" {
		params[paramContract] = contract
	}
	if orderID > 0 {
		params["order"] = strconv.FormatInt(orderID, 10)
	}
	params["limit"] = "100"
	body, err := c.GetOrderDealsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	var result []gateMyTrade
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) rawGetAccountBook(ctx context.Context, settle, contract, changeType string, startTime time.Time) ([]gateAccountBook, error) {
	params := map[string]string{
		paramSettle: settle,
	}
	if contract != "" {
		params[paramContract] = contract
	}
	if changeType != "" {
		params["type"] = changeType
	}
	if !startTime.IsZero() {
		params["from"] = strconv.FormatInt(startTime.Unix(), 10)
	}
	params["limit"] = "100"
	body, err := c.GetAccountBook(ctx, params)
	if err != nil {
		return nil, err
	}
	var result []gateAccountBook
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) rawSetPositionMode(ctx context.Context, settle string, dualMode bool) error {
	params := map[string]string{
		"dual_mode": strconv.FormatBool(dualMode),
	}
	path := fmt.Sprintf("/futures/%s/dual_mode", settle)
	_, err := c.RawRequest(ctx, "POST", path, params, nil)
	return err
}

// Public mapper methods.

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	if symbol != "" {
		rawPositions, err := c.rawGetPosition(ctx, gatePositionsRequest{
			Settle: gateSettleUsdt,
			Symbol: symbol,
		})
		if err != nil {
			if apiErr, ok := exchange.IsAPIError(err); ok && (apiErr.StatusCode == http.StatusNotFound || strings.Contains(strings.ToLower(apiErr.Message), "not found")) {
				return nil, nil
			}
			return nil, fmt.Errorf("gate.io get position for %s: %w", symbol, err)
		}

		positions := make([]exchange.Position, 0, len(rawPositions))
		for i := range rawPositions {
			pos := &rawPositions[i]
			sizeVal, _ := pos.Size.Int64()
			if sizeVal != 0 {
				positions = append(positions, mapPosition(*pos))
			}
		}
		return positions, nil
	}

	rawPositions, err := c.rawGetPositions(ctx, gatePositionsRequest{Settle: gateSettleUsdt})
	if err != nil {
		return nil, fmt.Errorf("gate.io list positions: %w", err)
	}

	positions := make([]exchange.Position, 0, len(rawPositions))
	for i := range rawPositions {
		pos := &rawPositions[i]
		sizeVal, _ := pos.Size.Int64()
		if sizeVal != 0 {
			positions = append(positions, mapPosition(*pos))
		}
	}
	return positions, nil
}

// GetOrderPNL queries personal trading records and account book history to map the closed position metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("gate get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	var startTime time.Time
	if orderInfo.CreateTime > 0 {
		startTime = time.UnixMilli(orderInfo.CreateTime - 1000)
	}

	closingTrades, err := c.waitAndFindClosingTrades(ctx, symbol, orderInfo.OrderID, startTime)
	if err != nil {
		return nil, fmt.Errorf("gate.io wait for closing trades failed: %w", err)
	}

	entryPrice, openFee := c.getOpeningTradesMetrics(ctx, symbol, orderInfo)
	if entryPrice == 0 {
		entryPrice = orderInfo.DealAvgPrice
		if entryPrice == 0 {
			entryPrice = orderInfo.Price
		}
	}

	exitPrice, sumVol, closeFee, latestTradeTime := c.calculateClosingTradesMetrics(closingTrades)

	totalFee := openFee + closeFee

	fundingFee, grossPnL := c.getLedgerMetrics(ctx, symbol, startTime)

	// Fallback mathematically if ledger PnL entries were not found
	if grossPnL == 0 {
		if orderInfo.Side == exchange.SideOpenLong {
			grossPnL = (exitPrice - entryPrice) * sumVol
		} else {
			grossPnL = (entryPrice - exitPrice) * sumVol
		}
	}
	netPnL := grossPnL - totalFee + fundingFee

	pnlRate := 0.0
	if entryPrice > 0 {
		if orderInfo.Side == exchange.SideOpenShort {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		}
	}

	durationMs := int64(0)
	if latestTradeTime > 0 {
		durationMs = max(int64(latestTradeTime*1000)-orderInfo.CreateTime, 0)
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: sumVol,
		GrossPnL:   grossPnL,
		Fee:        totalFee,
		FundingFee: fundingFee,
		DurationMs: durationMs,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
	}, nil
}

// ClosePosition submits a market order to close an open position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	orderSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		orderSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         orderSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "gate"),
		Leverage:     leverage,
	})
	if err != nil {
		return fmt.Errorf("gate.io close position: %w", err)
	}
	return nil
}

// CloseAllPositions closes all open positions for the given symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := &positions[i]
		if pos.HoldVol > 0 {
			var side domain.Side
			if pos.PositionType == exchange.PositionTypeLong { // Long
				side = domain.SideCloseLong
			} else { // Short
				side = domain.SideCloseShort
			}
			posErr := c.ClosePosition(ctx, symbol, side, pos.HoldVol, domain.PositionModeHedge, pos.Leverage) // default hedge mode close
			if posErr != nil {
				return posErr
			}
		}
	}
	return nil
}

// SetPositionMode sets the position mode (Hedge Mode vs One-Way Mode) on Gate.io.
func (c *Client) SetPositionMode(ctx context.Context, settle string, dualMode bool) error {
	return c.rawSetPositionMode(ctx, settle, dualMode)
}

func (c *Client) calculateClosingTradesMetrics(closingTrades []gateMyTrade) (exitPrice, sumVol, closeFee, latestTradeTime float64) {
	var sumPriceVol float64
	for i := range closingTrades {
		trade := &closingTrades[i]
		sizeVal, _ := trade.Size.Float64()
		priceVal, _ := trade.Price.Float64()
		feeVal, _ := trade.Fee.Float64()

		szAbs := math.Abs(sizeVal)
		sumPriceVol += priceVal * szAbs
		sumVol += szAbs
		closeFee += math.Abs(feeVal)

		if trade.CreateTime > latestTradeTime {
			latestTradeTime = trade.CreateTime
		}
	}
	if sumVol > 0 {
		exitPrice = sumPriceVol / sumVol
	}
	return
}

func (c *Client) getOpeningTradesMetrics(ctx context.Context, symbol string, orderInfo *exchange.OrderInfo) (entryPrice, openFee float64) {
	openOrderID, err := strconv.ParseInt(orderInfo.OrderID, 10, 64)
	if err != nil || openOrderID <= 0 {
		return
	}

	openTrades, err := c.rawGetMyTrades(ctx, gateSettleUsdt, symbol, openOrderID)
	if err != nil || len(openTrades) == 0 {
		return
	}

	var sumOpenPriceVol float64
	var sumOpenVol float64
	for i := range openTrades {
		trade := &openTrades[i]
		feeVal, _ := trade.Fee.Float64()
		openFee += math.Abs(feeVal)

		sz, _ := trade.Size.Float64()
		px, _ := trade.Price.Float64()
		szAbs := math.Abs(sz)
		sumOpenPriceVol += px * szAbs
		sumOpenVol += szAbs
	}

	if sumOpenVol > 0 {
		entryPrice = sumOpenPriceVol / sumOpenVol
	}
	return
}

func (c *Client) getLedgerMetrics(ctx context.Context, symbol string, startTime time.Time) (fundingFee, grossPnL float64) {
	ledgerEntries, err := c.rawGetAccountBook(ctx, gateSettleUsdt, symbol, "", startTime)
	if err != nil {
		return
	}
	for i := range ledgerEntries {
		entry := &ledgerEntries[i]
		changeVal, _ := strconv.ParseFloat(entry.Change, 64)
		switch entry.Type {
		case "fund":
			fundingFee += changeVal
		case "pnl":
			grossPnL += changeVal
		}
	}
	return
}

func (c *Client) waitAndFindClosingTrades(ctx context.Context, symbol, openingOrderID string, startTime time.Time) ([]gateMyTrade, error) {
	trades, err := c.rawGetMyTrades(ctx, gateSettleUsdt, symbol, 0)
	if err != nil {
		return nil, err
	}

	startUnix := startTime.Unix()
	var found []gateMyTrade
	for i := range trades {
		trade := &trades[i]
		if int64(trade.CreateTime) >= startUnix {
			closeSizeVal, _ := trade.CloseSize.Float64()
			if closeSizeVal != 0 && trade.OrderID != openingOrderID {
				found = append(found, *trade)
			}
		}
	}

	if len(found) == 0 {
		return nil, fmt.Errorf("no closing trades found for symbol %s since %v", symbol, startTime)
	}

	return found, nil
}

// mapPosition maps a gatePosition to exchange.Position.
func mapPosition(raw gatePosition) exchange.Position {
	sizeVal, _ := raw.Size.Int64()
	levVal, _ := raw.Leverage.Int64()
	pos := exchange.Position{
		Symbol:       raw.Contract,
		HoldVol:      float64(decmath.AbsInt64(sizeVal)),
		HoldAvgPrice: decmath.ParseFloat(raw.EntryPrice),
		OpenAvgPrice: decmath.ParseFloat(raw.EntryPrice),
		Leverage:     int(levVal),
	}

	if sizeVal > 0 {
		pos.PositionType = exchange.PositionTypeLong // Long.
	} else if sizeVal < 0 {
		pos.PositionType = exchange.PositionTypeShort // Short.
	}

	return pos
}
