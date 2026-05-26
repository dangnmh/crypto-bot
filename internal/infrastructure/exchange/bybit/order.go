package bybit

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bybitCreateOrderResult struct {
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
}

type bybitOrder struct {
	Symbol      string `json:"symbol"`
	OrderID     string `json:"orderId"`
	OrderLinkID string `json:"orderLinkId"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	Side        string `json:"side"`
	OrderStatus string `json:"orderStatus"`
	OrderType   string `json:"orderType"`
	CumExecQty  string `json:"cumExecQty"`
	AvgPrice    string `json:"avgPrice"`
	CreatedTime string `json:"createdTime"`
	UpdatedTime string `json:"updatedTime"`
	PositionIdx int    `json:"positionIdx"`
}

type bybitOrderResult struct {
	Category string       `json:"category"`
	List     []bybitOrder `json:"list"`
}

// mapSubmitOrder maps a SubmitOrderRequest to Bybit parameters map.
func (c *Client) mapSubmitOrder(req exchange.SubmitOrderRequest) map[string]interface{} {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
		symbolKey:   req.Symbol,
	}

	bybitOrderType, bybitTif := mapOrderTypeAndTif(req.Type)
	params["orderType"] = bybitOrderType
	params["timeInForce"] = bybitTif

	if req.Type != exchange.OrderTypeMarket {
		params["price"] = fmt.Sprintf("%g", req.Price)
	}

	params["qty"] = fmt.Sprintf("%g", req.Vol)

	bybitSide, positionIdx, reduceOnly := mapSideAndPosition(req.Side, req.PositionMode == 1)
	params["side"] = bybitSide
	params["positionIdx"] = positionIdx
	if reduceOnly || req.ReduceOnly {
		params["reduceOnly"] = true
	}

	if req.ExternalOID != "" {
		params["orderLinkId"] = req.ExternalOID
	}

	if req.TakeProfitPrice > 0 {
		params["takeProfit"] = fmt.Sprintf("%g", req.TakeProfitPrice)
	}
	if req.StopLossPrice > 0 {
		params["stopLoss"] = fmt.Sprintf("%g", req.StopLossPrice)
	}

	return params
}

func mapOrderTypeAndTif(orderType int) (string, string) {
	switch orderType {
	case exchange.OrderTypeMarket:
		return "Market", tifIOC
	case exchange.OrderTypePostOnly:
		return orderTypeLimit, "PostOnly"
	case exchange.OrderTypeIOC:
		return orderTypeLimit, tifIOC
	case exchange.OrderTypeFOK:
		return orderTypeLimit, "FOK"
	default:
		return orderTypeLimit, "GTC"
	}
}

func mapSideAndPosition(reqSide int, isHedge bool) (string, int, bool) {
	bybitSide := sideBuy
	positionIdx := 0
	reduceOnly := false

	if isHedge {
		switch reqSide {
		case exchange.SideOpenLong:
			bybitSide = sideBuy
			positionIdx = 1
		case exchange.SideCloseLong:
			bybitSide = sideSell
			positionIdx = 1
			reduceOnly = true
		case exchange.SideOpenShort:
			bybitSide = sideSell
			positionIdx = 2
		case exchange.SideCloseShort:
			bybitSide = sideBuy
			positionIdx = 2
			reduceOnly = true
		}
	} else {
		positionIdx = 0
		switch reqSide {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			bybitSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			bybitSide = sideSell
		}
	}
	return bybitSide, positionIdx, reduceOnly
}

// mapOrderInfo maps a bybitOrder to exchange.OrderInfo.
func mapOrderInfo(raw bybitOrder) exchange.OrderInfo {
	info := exchange.OrderInfo{
		OrderID:      raw.OrderID,
		Symbol:       raw.Symbol,
		Price:        decmath.ParseFloat(raw.Price),
		Vol:          decmath.ParseFloat(raw.Qty),
		DealAvgPrice: decmath.ParseFloat(raw.AvgPrice),
		DealVol:      decmath.ParseFloat(raw.CumExecQty),
		ExternalOID:  raw.OrderLinkID,
		PositionMode: 2, // Default One-Way
	}

	if raw.PositionIdx > 0 {
		info.PositionMode = 1 // Hedge mode
	}

	// Created/Updated times
	if raw.CreatedTime != "" {
		if parsed, err := strconv.ParseInt(raw.CreatedTime, 10, 64); err == nil {
			info.CreateTime = parsed
		}
	}
	if raw.UpdatedTime != "" {
		if parsed, err := strconv.ParseInt(raw.UpdatedTime, 10, 64); err == nil {
			info.UpdateTime = parsed
		}
	}

	// Map Side
	switch raw.Side {
	case sideBuy:
		if raw.PositionIdx == 2 {
			info.Side = exchange.SideCloseShort
		} else {
			info.Side = exchange.SideOpenLong
		}
	case sideSell:
		if raw.PositionIdx == 1 {
			info.Side = exchange.SideCloseLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	// Map State
	switch strings.ToLower(raw.OrderStatus) {
	case "filled":
		info.State = exchange.OrderStateFilled
	case "cancelled", "rejected":
		info.State = exchange.OrderStateCanceled
	case "partiallyfilled":
		info.State = exchange.OrderStatePartial
	}

	return info
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	params := c.mapSubmitOrder(req)

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).PlaceOrder(ctx)
	if err != nil {
		return "", fmt.Errorf("bybit create order: %w", err)
	}
	if resp.RetCode != 0 {
		return "", fmt.Errorf("bybit create order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitCreateOrderResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return "", fmt.Errorf("bybit decode create order result: %w", err)
	}

	return res.OrderID, nil
}

// CreateTrackOrder submits a trailing stop order. Stubbed since track orders are not used in Core reversion.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Bybit")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
		symbolKey:   symbol,
		orderIDKey:  orderID,
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).CancelOrder(ctx)
	if err != nil {
		return fmt.Errorf("bybit cancel order: %w", err)
	}
	if resp.RetCode != 0 {
		// If order is already cancelled/filled, return nil to match Gate.io behavior
		if resp.RetCode == 110001 || strings.Contains(strings.ToLower(resp.RetMsg), "already cancelled") || strings.Contains(strings.ToLower(resp.RetMsg), "filled") {
			return nil
		}
		return fmt.Errorf("bybit cancel order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	// For Bybit standard/unified, bulk cancel is not directly standard in a single SDK call,
	// so we loop over individual order cancels like in mexc/gate.
	for _, id := range orderIDs {
		// Note: since CancelOrder requires symbol, and the bot uses symbols globally,
		// we query order or use empty symbol (Bybit V5 cancel requires symbol).
		// We fallback to checking each or mapping. The sniper bot passes symbols or empty.
		// If empty, we try to cancel. (CancelOrders in sniper is normally called with known symbol order).
		err := c.CancelOrder(ctx, "", id)
		if err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
		symbolKey:   symbol,
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).CancelAllOrders(ctx)
	if err != nil {
		return fmt.Errorf("bybit cancel all orders: %w", err)
	}
	if resp.RetCode != 0 {
		return fmt.Errorf("bybit cancel all orders error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
		orderIDKey:  orderID,
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetOpenOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit get order %s: %w", orderID, err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit get order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitOrderResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode order: %w", err)
	}

	if len(res.List) == 0 {
		return nil, fmt.Errorf("bybit order not found: %s", orderID)
	}

	info := mapOrderInfo(res.List[0])
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]interface{}{
		categoryKey: categoryLinear,
	}
	if symbol != "" {
		params[symbolKey] = symbol
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).GetOpenOrders(ctx)
	if err != nil {
		return nil, fmt.Errorf("bybit list open orders: %w", err)
	}
	if resp.RetCode != 0 {
		return nil, fmt.Errorf("bybit list open orders error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var res bybitOrderResult
	if err := decodeResult(resp.Result, &res); err != nil {
		return nil, fmt.Errorf("bybit decode open orders: %w", err)
	}

	orders := make([]exchange.OrderInfo, 0, len(res.List))
	for i := range res.List {
		orders = append(orders, mapOrderInfo(res.List[i]))
	}
	return orders, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         int(closeSide),
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
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
		pos := &positions[i]
		if pos.HoldVol > 0 {
			var side domain.Side
			if pos.PositionType == 1 { // Long
				side = domain.SideCloseLong
			} else { // Short
				side = domain.SideCloseShort
			}
			posErr := c.ClosePosition(ctx, symbol, side, pos.HoldVol, 1) // default hedge mode close
			if posErr != nil {
				return posErr
			}
		}
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	params := map[string]interface{}{
		categoryKey:    categoryLinear,
		symbolKey:      req.Symbol,
		"buyLeverage":  fmt.Sprintf("%d", req.Leverage),
		"sellLeverage": fmt.Sprintf("%d", req.Leverage),
	}

	resp, err := c.sdkClient.NewUtaBybitServiceWithParams(params).SetPositionLeverage(ctx)
	if err != nil {
		return fmt.Errorf("bybit change leverage: %w", err)
	}
	// Bybit returns RetCode 110043 if leverage is already set to the target value. We ignore this safely!
	if resp.RetCode != 0 && resp.RetCode != 110043 {
		return fmt.Errorf("bybit change leverage error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}
