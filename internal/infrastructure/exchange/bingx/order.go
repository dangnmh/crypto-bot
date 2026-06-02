package bingx

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for order endpoints.

type bingxCreateOrderRequest struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	Type          string `json:"type"`
	Quantity      string `json:"quantity"`
	Price         string `json:"price,omitempty"`
	ClientOrderID string `json:"clientOrderID,omitempty"`
}

type bingxCancelOrderRequest struct {
	Symbol  string `json:"symbol"`
	OrderID string `json:"orderId"`
}

type bingxGetOrderRequest struct {
	Symbol  string `json:"symbol"`
	OrderID string `json:"orderId"`
}

type bingxListOpenOrdersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type bingxChangeLeverageRequest struct {
	Symbol       string `json:"symbol"`
	Leverage     string `json:"leverage"`
	PositionSide string `json:"positionSide"`
}

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

// Private raw methods invoking the BingX REST API.

func (c *Client) createRawOrder(ctx context.Context, req bingxCreateOrderRequest) (*bingxOrderResult, error) {
	bodyMap := map[string]any{
		paramSymbol:       req.Symbol,
		"side":            req.Side,
		paramPositionSide: req.PositionSide,
		"type":            req.Type,
		"quantity":        req.Quantity,
	}

	if req.Price != "" {
		bodyMap["price"] = req.Price
	}

	if req.ClientOrderID != "" {
		bodyMap["clientOrderID"] = req.ClientOrderID
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bingxOrderResult](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req bingxCancelOrderRequest) error {
	bodyMap := map[string]any{
		paramSymbol:  req.Symbol,
		paramOrderId: req.OrderID,
	}

	body, err := c.PostCtx(ctx, pathCancelOrder, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "cancel_order")
}

func (c *Client) getRawOrder(ctx context.Context, req bingxGetOrderRequest) (*bingxOrder, error) {
	params := map[string]string{
		paramSymbol:  req.Symbol,
		paramOrderId: req.OrderID,
	}

	body, err := c.GetCtx(ctx, pathGetOrder, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bingxOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req bingxListOpenOrdersRequest) ([]bingxOrder, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bingxOrder](body, "open_orders")
}

func (c *Client) changeRawLeverage(ctx context.Context, req bingxChangeLeverageRequest) error {
	bodyMap := map[string]any{
		paramSymbol:       req.Symbol,
		paramLeverage:     req.Leverage,
		paramPositionSide: req.PositionSide,
	}
	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "set_leverage")
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType := mapOrderType(req.Type)
	side, posSide := mapSideAndPosSide(req.Side, req.PositionMode)

	rawReq := bingxCreateOrderRequest{
		Symbol:       req.Symbol,
		Side:         side,
		PositionSide: posSide,
		Type:         ordType,
		Quantity:     fmt.Sprintf("%g", req.Vol),
	}

	if req.Type != exchange.OrderTypeMarket {
		rawReq.Price = fmt.Sprintf("%g", req.Price)
	}

	if req.ExternalOID != "" {
		rawReq.ClientOrderID = req.ExternalOID
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	return exchange.CreateOrderResult{
		OrderID:       res.OrderID,
		TPSLSubmitted: false,
	}, nil
}

// CreateTrackOrder is a placeholder.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented on BingX")
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return c.cancelRawOrder(ctx, bingxCancelOrderRequest{
		Symbol:  symbol,
		OrderID: orderID,
	})
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
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrder(ctx, bingxGetOrderRequest{
		Symbol:  symbol,
		OrderID: orderID,
	})
	if err != nil {
		return nil, err
	}

	return c.toOrderInfo(raw), nil
}

// GetOpenOrders returns all currently active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.getRawOpenOrders(ctx, bingxListOpenOrdersRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	infos := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		infos = append(infos, *c.toOrderInfo(&rawList[i]))
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
		if pos.PositionType == 2 { // 2 = Short.
			closeSide = domain.SideCloseShort
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1)
	}

	return nil
}

// ChangeLeverage changes leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	levStr := strconv.Itoa(req.Leverage)

	// Set for LONG.
	err := c.changeRawLeverage(ctx, bingxChangeLeverageRequest{
		Symbol:       req.Symbol,
		Leverage:     levStr,
		PositionSide: posSideLong,
	})
	if err != nil {
		return err
	}

	// Set for SHORT.
	return c.changeRawLeverage(ctx, bingxChangeLeverageRequest{
		Symbol:       req.Symbol,
		Leverage:     levStr,
		PositionSide: posSideShort,
	})
}

// Helper mapping methods.

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

func (c *Client) toOrderInfo(o *bingxOrder) *exchange.OrderInfo {
	state := 0 // default active/pending.
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
