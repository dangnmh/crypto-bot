package okx

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type okxOrderResult struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

type okxOrder struct {
	InstID  string `json:"instId"`
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	Px      string `json:"px"`
	Sz      string `json:"sz"`
	Side    string `json:"side"`
	PosSide string `json:"posSide"`
	State   string `json:"state"`
	OrdType string `json:"ordType"`
	AccRe   string `json:"accRe"`
	AvgPx   string `json:"avgPx"`
	UTime   string `json:"uTime"`
	CTime   string `json:"cTime"`
	FillSz  string `json:"fillSz"`
	TradeId string `json:"tradeId"`
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	// Map order type
	ordType := "limit"
	switch req.Type {
	case exchange.OrderTypeMarket:
		ordType = "market"
	case exchange.OrderTypePostOnly:
		ordType = "post_only"
	case exchange.OrderTypeIOC:
		ordType = "ioc"
	case exchange.OrderTypeFOK:
		ordType = "fok"
	}

	// Map side and position side
	side := sideBuy
	posSide := posSideLong
	isHedge := req.PositionMode == 1 || req.PositionMode == 0

	if isHedge {
		switch req.Side {
		case exchange.SideOpenLong:
			side = sideBuy
			posSide = posSideLong
		case exchange.SideCloseLong:
			side = sideSell
			posSide = posSideLong
		case exchange.SideOpenShort:
			side = sideSell
			posSide = posSideShort
		case exchange.SideCloseShort:
			side = sideBuy
			posSide = posSideShort
		}
	} else {
		posSide = posSideNet
		switch req.Side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			side = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			side = sideSell
		}
	}

	tdMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		tdMode = modeCross
	}

	bodyMap := map[string]interface{}{
		"instId":  req.Symbol,
		"tdMode":  tdMode,
		"side":    side,
		"ordType": ordType,
		"sz":      fmt.Sprintf("%g", req.Vol),
	}

	if isHedge {
		bodyMap["posSide"] = posSide
	}

	if req.Type != exchange.OrderTypeMarket {
		bodyMap["px"] = fmt.Sprintf("%g", req.Price)
	}

	if req.ExternalOID != "" {
		bodyMap["clOrdId"] = req.ExternalOID
	}

	if req.ReduceOnly {
		bodyMap["reduceOnly"] = true
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return "", err
	}

	res, err := ParseResponseFirst[okxOrderResult](body, "create_order")
	if err != nil {
		return "", err
	}

	if res.SCode != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(res.SCode, "%d", &codeVal)
		return "", toAPIError(codeVal, res.SMsg, "create_order")
	}

	return res.OrdID, nil
}

// CreateTrackOrder submits a trailing stop order (stubbed/not implemented).
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for OKX")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if symbol == "" {
		// If symbol is not known, find it first
		info, err := c.GetOrder(ctx, orderID)
		if err != nil {
			return fmt.Errorf("cancel order failed to locate order: %w", err)
		}
		symbol = info.Symbol
	}

	bodyMap := map[string]interface{}{
		"instId": symbol,
		"ordId":  orderID,
	}

	body, err := c.PostCtx(ctx, pathCancelOrder, bodyMap)
	if err != nil {
		return err
	}

	res, err := ParseResponseFirst[okxOrderResult](body, "cancel_order")
	if err != nil {
		return err
	}

	if res.SCode != "0" {
		// If already cancelled or filled, treat as success to match expected behavior.
		if res.SCode == "51400" || res.SCode == "51401" || strings.Contains(strings.ToLower(res.SMsg), "already") || strings.Contains(strings.ToLower(res.SMsg), "filled") {
			return nil
		}
		codeVal := 0
		_, _ = fmt.Sscanf(res.SCode, "%d", &codeVal)
		return toAPIError(codeVal, res.SMsg, "cancel_order")
	}

	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for i := range orderIDs {
		err := c.CancelOrder(ctx, "", orderIDs[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	orders, err := c.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range orders {
		err := c.CancelOrder(ctx, symbol, orders[i].OrderID)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	// As OKX V5 trade/order GET requires instId, we search active and history orders
	// 1. Search pending orders
	pendingBody, err := c.GetCtx(ctx, pathPendingOrders, map[string]string{"instType": "SWAP"})
	if err == nil {
		pendingList, parseErr := ParseResponse[okxOrder](pendingBody, "pending_orders")
		if parseErr == nil {
			for i := range pendingList {
				o := pendingList[i]
				if o.OrdID == orderID {
					info := mapOkxOrder(o)
					return &info, nil
				}
			}
		}
	}

	// 2. Search history orders
	historyBody, err := c.GetCtx(ctx, "/api/v5/trade/orders-history", map[string]string{"instType": "SWAP"})
	if err == nil {
		historyList, parseErr := ParseResponse[okxOrder](historyBody, "orders_history")
		if parseErr == nil {
			for i := range historyList {
				o := historyList[i]
				if o.OrdID == orderID {
					info := mapOkxOrder(o)
					return &info, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("order not found: %s", orderID)
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{
		"instType": "SWAP",
	}
	if symbol != "" {
		params["instId"] = symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	list, err := ParseResponse[okxOrder](body, "open_orders")
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(list))
	for i := range list {
		orders = append(orders, mapOkxOrder(list[i]))
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
		pos := positions[i]
		if pos.HoldVol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == 1 { // Long
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, pos.HoldVol, 1)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mgnMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		mgnMode = modeCross
	}

	bodyMap := map[string]interface{}{
		"instId":  req.Symbol,
		"lever":   fmt.Sprintf("%d", req.Leverage),
		"mgnMode": mgnMode,
	}

	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "change_leverage")
}

func mapOkxOrder(o okxOrder) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(o.Px, 64)
	sz, _ := strconv.ParseFloat(o.Sz, 64)
	avgPx, _ := strconv.ParseFloat(o.AvgPx, 64)
	fillSz, _ := strconv.ParseFloat(o.FillSz, 64)
	uTime, _ := strconv.ParseInt(o.UTime, 10, 64)
	cTime, _ := strconv.ParseInt(o.CTime, 10, 64)

	info := exchange.OrderInfo{
		OrderID:      o.OrdID,
		Symbol:       o.InstID,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClOrdID,
		CreateTime:   cTime,
		UpdateTime:   uTime,
		PositionMode: 2, // default OneWay
	}

	switch o.PosSide {
	case posSideLong:
		info.PositionMode = 1
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideCloseLong
		}
	case posSideShort:
		info.PositionMode = 1
		if o.Side == sideSell {
			info.Side = exchange.SideOpenShort
		} else {
			info.Side = exchange.SideCloseShort
		}
	default:
		// net mode
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	switch o.State {
	case stateFilled:
		info.State = exchange.OrderStateFilled
	case stateCanceled:
		info.State = exchange.OrderStateCanceled
	default:
		info.State = exchange.OrderStatePartial
	}

	return info
}
