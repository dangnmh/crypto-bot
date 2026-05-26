package bingx

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type bingxOrderResult struct {
	OrderID   interface{} `json:"orderId"`
	ClientOid string      `json:"clientOid"`
}

type bingxOrder struct {
	OrderID      interface{} `json:"orderId"`
	ClientOid    string      `json:"clientOid"`
	Symbol       string      `json:"symbol"`
	Side         string      `json:"side"`
	PositionSide string      `json:"positionSide"`
	Type         string      `json:"type"`
	Quantity     interface{} `json:"quantity"`
	Price        interface{} `json:"price"`
	Status       string      `json:"status"`
	ExecutedQty  interface{} `json:"executedQty"`
	AvgPrice     interface{} `json:"avgPrice"`
	Time         interface{} `json:"time"`
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	ordType := "LIMIT"
	switch req.Type {
	case exchange.OrderTypeMarket:
		ordType = "MARKET"
	case exchange.OrderTypePostOnly:
		ordType = "POST_ONLY"
	case exchange.OrderTypeIOC:
		ordType = "IOC"
	case exchange.OrderTypeFOK:
		ordType = "FOK"
	}

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
		switch req.Side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			side = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			side = sideSell
		}
		posSide = "BOTH"
	}

	bodyMap := map[string]interface{}{
		paramSymbol:    req.Symbol,
		"side":         side,
		"positionSide": posSide,
		"type":         ordType,
		"quantity":     fmt.Sprintf("%g", req.Vol),
	}

	if req.Type != exchange.OrderTypeMarket {
		bodyMap["price"] = fmt.Sprintf("%g", req.Price)
	}

	if req.ExternalOID != "" {
		bodyMap["clientOrderID"] = req.ExternalOID
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return "", err
	}

	res, err := ParseResponse[bingxOrderResult](body, "create_order")
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("%v", res.OrderID), nil
}

// CreateTrackOrder is a placeholder.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented on BingX")
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	bodyMap := map[string]interface{}{
		paramSymbol: symbol,
		"orderId":   orderID,
	}

	body, err := c.PostCtx(ctx, pathCancelOrder, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "cancel_order")
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch CancelOrders not implemented on BingX")
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	orders, err := c.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	for _, ord := range orders {
		_ = c.CancelOrder(ctx, symbol, ord.OrderID)
	}

	return nil
}

// GetOrder fetches details of a specific order.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		"orderId": orderID,
	}

	body, err := c.GetCtx(ctx, pathGetOrder, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bingxOrder](body, "get_order")
	if err != nil {
		return nil, err
	}

	return c.toOrderInfo(&res), nil
}

// GetOpenOrders returns all currently active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[[]bingxOrder](body, "open_orders")
	if err != nil {
		return nil, err
	}

	infos := make([]exchange.OrderInfo, 0, len(res))
	for i := range res {
		infos = append(infos, *c.toOrderInfo(&res[i]))
	}

	return infos, nil
}

// ClosePosition is a helper to close a position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
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
	})
	return err
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for _, pos := range positions {
		closeSide := domain.SideCloseLong
		if pos.PositionType == 2 { // 2 = Short
			closeSide = domain.SideCloseShort
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1)
	}

	return nil
}

// ChangeLeverage changes leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	// Set for LONG
	bodyMapLong := map[string]interface{}{
		paramSymbol:    req.Symbol,
		"leverage":     strconv.Itoa(req.Leverage),
		"positionSide": "LONG",
	}
	bodyLong, err := c.PostCtx(ctx, pathSetLeverage, bodyMapLong)
	if err != nil {
		return err
	}
	if err := ParseResponseIgnoreData(bodyLong, "set_leverage_long"); err != nil {
		return err
	}

	// Set for SHORT
	bodyMapShort := map[string]interface{}{
		paramSymbol:    req.Symbol,
		"leverage":     strconv.Itoa(req.Leverage),
		"positionSide": "SHORT",
	}
	bodyShort, err := c.PostCtx(ctx, pathSetLeverage, bodyMapShort)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(bodyShort, "set_leverage_short")
}

func (c *Client) toOrderInfo(o *bingxOrder) *exchange.OrderInfo {
	state := 0 // default active/pending
	switch o.Status {
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	case statePartFill:
		state = exchange.OrderStatePartial
	}

	sideVal := exchange.SideOpenLong
	switch o.Side {
	case sideBuy:
		if o.PositionSide == posSideLong {
			sideVal = exchange.SideOpenLong
		} else {
			sideVal = exchange.SideCloseShort
		}
	case sideSell:
		if o.PositionSide == posSideLong {
			sideVal = exchange.SideCloseLong
		} else {
			sideVal = exchange.SideOpenShort
		}
	}

	price := parseFloat(o.Price)
	qty := parseFloat(o.Quantity)
	exec := parseFloat(o.ExecutedQty)
	avg := parseFloat(o.AvgPrice)

	return &exchange.OrderInfo{
		OrderID:      fmt.Sprintf("%v", o.OrderID),
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          qty,
		DealVol:      exec,
		DealAvgPrice: avg,
		State:        state,
		Side:         sideVal,
	}
}
