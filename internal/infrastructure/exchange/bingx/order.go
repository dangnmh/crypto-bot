package bingx

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bingxOrderResult struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type bingxOrder struct {
	OrderID      string `json:"orderId"`
	ClientOid    string `json:"clientOid"`
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`
	PositionSide string `json:"positionSide"`
	Type         string `json:"type"`
	Quantity     string `json:"quantity"`
	Price        string `json:"price"`
	Status       string `json:"status"`
	ExecutedQty  string `json:"executedQty"`
	AvgPrice     string `json:"avgPrice"`
	Time         string `json:"time"`
}

func mapOrderType(t int) string {
	switch t {
	case exchange.OrderTypeMarket:
		return "MARKET"
	case exchange.OrderTypePostOnly:
		return "POST_ONLY"
	case exchange.OrderTypeIOC:
		return "IOC"
	case exchange.OrderTypeFOK:
		return "FOK"
	default:
		return "LIMIT"
	}
}

func mapSideAndPosSide(side, posMode int) (string, string) {
	ordSide := sideBuy
	posSide := posSideLong
	isHedge := posMode == 1 || posMode == 0

	if isHedge {
		switch side {
		case exchange.SideOpenLong:
			ordSide = sideBuy
			posSide = posSideLong
		case exchange.SideCloseLong:
			ordSide = sideSell
			posSide = posSideLong
		case exchange.SideOpenShort:
			ordSide = sideSell
			posSide = posSideShort
		case exchange.SideCloseShort:
			ordSide = sideBuy
			posSide = posSideShort
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			ordSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			ordSide = sideSell
		}
		posSide = posSideBoth
	}
	return ordSide, posSide
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	ordType := mapOrderType(req.Type)
	side, posSide := mapSideAndPosSide(req.Side, req.PositionMode)

	bodyMap := map[string]interface{}{
		paramSymbol:       req.Symbol,
		"side":            side,
		paramPositionSide: posSide,
		"type":            ordType,
		"quantity":        fmt.Sprintf("%g", req.Vol),
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

	return res.OrderID, nil
}

// CreateTrackOrder is a placeholder.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented on BingX")
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	bodyMap := map[string]interface{}{
		paramSymbol:  symbol,
		paramOrderId: orderID,
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

	for i := range orders {
		_ = c.CancelOrder(ctx, symbol, orders[i].OrderID)
	}

	return nil
}

// GetOrder fetches details of a specific order.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		paramOrderId: orderID,
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

	for i := range positions {
		pos := &positions[i]
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
		paramSymbol:       req.Symbol,
		paramLeverage:     strconv.Itoa(req.Leverage),
		paramPositionSide: posSideLong,
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
		paramSymbol:       req.Symbol,
		paramLeverage:     strconv.Itoa(req.Leverage),
		paramPositionSide: posSideShort,
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

	price := decmath.ParseFloat(o.Price)
	qty := decmath.ParseFloat(o.Quantity)
	exec := decmath.ParseFloat(o.ExecutedQty)
	avg := decmath.ParseFloat(o.AvgPrice)

	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          qty,
		DealVol:      exec,
		DealAvgPrice: avg,
		State:        state,
		Side:         sideVal,
	}
}
