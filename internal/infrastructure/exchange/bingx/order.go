package bingx

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for order endpoints.

type bingxCreateOrderRequest struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	PositionSide  string `json:"positionSide"`
	Type          string `json:"type"`
	Quantity      string `json:"quantity"`
	Price         string `json:"price,omitempty"`
	TimeInForce   string `json:"timeInForce,omitempty"`
	ClientOrderID string `json:"clientOrderId,omitempty"`
	TakeProfit    string `json:"takeProfit,omitempty"`
	StopLoss      string `json:"stopLoss,omitempty"`
}

type bingxCancelOrderRequest struct {
	Symbol  string `json:"symbol"`
	OrderID string `json:"orderId"`
}

type bingxGetOrderRequest struct {
	Symbol        string `json:"symbol"`
	OrderID       string `json:"orderId,omitempty"`
	ClientOrderID string `json:"clientOrderId,omitempty"`
}

type bingxListOpenOrdersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type bingxChangeLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage string `json:"leverage"`
	Side     string `json:"side"`
}

type bingxCreateOrderResponse struct {
	Order struct {
		OrderID       xjson.Number `json:"orderId"`
		UpperOrderID  string       `json:"orderID"`
		ClientOrderID string       `json:"clientOrderId"`
	} `json:"order"`
}

type bingxGetOrderResponse struct {
	Order bingxOrder `json:"order"`
}

type bingxOrder struct {
	OrderID      xjson.Number `json:"orderId"`
	ClientOid    string       `json:"clientOid"`
	Symbol       string       `json:"symbol"`
	Side         string       `json:"side"`
	PositionSide string       `json:"positionSide"`
	Type         string       `json:"type"`
	Quantity     string       `json:"quantity"`
	OrigQty      string       `json:"origQty"`
	Price        string       `json:"price"`
	Status       string       `json:"status"`
	ExecutedQty  string       `json:"executedQty"`
	AvgPrice     string       `json:"avgPrice"`
	Time         int64        `json:"time"`
}

// Private raw methods invoking the BingX REST API.

func (c *Client) createRawOrder(ctx context.Context, req bingxCreateOrderRequest) (*bingxCreateOrderResponse, error) {
	bodyMap := map[string]any{
		paramSymbol:       req.Symbol,
		paramSide:         req.Side,
		paramPositionSide: req.PositionSide,
		paramType:         req.Type,
		"quantity":        req.Quantity,
	}

	if req.Price != "" {
		bodyMap[paramPrice] = req.Price
	}

	if req.TimeInForce != "" {
		bodyMap["timeInForce"] = req.TimeInForce
	}

	if req.ClientOrderID != "" {
		bodyMap["clientOrderId"] = req.ClientOrderID
	}

	if req.TakeProfit != "" {
		bodyMap["takeProfit"] = req.TakeProfit
	}

	if req.StopLoss != "" {
		bodyMap["stopLoss"] = req.StopLoss
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, nil, bodyMap)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bingxCreateOrderResponse](body, "create_order")
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

	body, err := c.PostCtx(ctx, pathCancelOrder, nil, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "cancel_order")
}

func (c *Client) getRawOrder(ctx context.Context, req bingxGetOrderRequest) (*bingxOrder, error) {
	params := map[string]string{
		paramSymbol: req.Symbol,
	}
	if req.OrderID != "" {
		params[paramOrderId] = req.OrderID
	}
	if req.ClientOrderID != "" {
		params["clientOrderId"] = req.ClientOrderID
	}

	body, err := c.GetCtx(ctx, pathGetOrder, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bingxGetOrderResponse](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &res.Order, nil
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
		paramSymbol:   req.Symbol,
		paramLeverage: req.Leverage,
		paramSide:     req.Side,
	}
	body, err := c.PostCtx(ctx, pathSetLeverage, nil, bodyMap)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "set_leverage")
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType, tif := mapOrderTypeAndTif(req.Type)
	side, posSide := mapSideAndPosSide(req.Side, req.PositionMode)

	rawReq := bingxCreateOrderRequest{
		Symbol:       req.Symbol,
		Side:         side,
		PositionSide: posSide,
		Type:         ordType,
		Quantity:     decmath.FormatFloat(req.Vol),
		TimeInForce:  tif,
	}

	if req.Type != exchange.OrderTypeMarket {
		rawReq.Price = decmath.FormatFloat(req.Price)
	}

	if req.ExternalOID != "" {
		rawReq.ClientOrderID = req.ExternalOID
	}

	tpslSubmitted := false
	if req.TakeProfitPrice > 0 {
		tpObj := map[string]any{
			paramType:        "TAKE_PROFIT_MARKET",
			paramStopPrice:   req.TakeProfitPrice,
			paramPrice:       req.TakeProfitPrice,
			paramWorkingType: valMarkPrice,
		}
		tpBytes, err := xjson.Marshal(tpObj)
		if err == nil {
			rawReq.TakeProfit = string(tpBytes)
			tpslSubmitted = true
		}
	}

	if req.StopLossPrice > 0 {
		slObj := map[string]any{
			paramType:        "STOP_MARKET",
			paramStopPrice:   req.StopLossPrice,
			paramPrice:       req.StopLossPrice,
			paramWorkingType: valMarkPrice,
		}
		slBytes, err := xjson.Marshal(slObj)
		if err == nil {
			rawReq.StopLoss = string(slBytes)
			tpslSubmitted = true
		}
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	orderIDStr := res.Order.OrderID.String()
	if orderIDStr == "0" {
		orderIDStr = res.Order.UpperOrderID
	}

	return exchange.CreateOrderResult{
		OrderID:       orderIDStr,
		TPSLSubmitted: tpslSubmitted,
	}, nil
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

// GetOrder fetches details of a specific order by exchange order ID.
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

// GetOrderByExternalID fetches details of a specific order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrder(ctx, bingxGetOrderRequest{
		Symbol:        symbol,
		ClientOrderID: externalOrderID,
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
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
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
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "bingx"),
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
		if pos.PositionType == exchange.PositionTypeShort { // Short.
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
		Symbol:   req.Symbol,
		Leverage: levStr,
		Side:     posSideLong,
	})
	if err != nil {
		return err
	}

	// Set for SHORT.
	return c.changeRawLeverage(ctx, bingxChangeLeverageRequest{
		Symbol:   req.Symbol,
		Leverage: levStr,
		Side:     posSideShort,
	})
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for BingX.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	const modeIsolated = "ISOLATED"
	mgnType := "CROSSED"
	if marginMode == modeIsolated {
		mgnType = modeIsolated
	}

	params := map[string]string{
		"symbol":     symbol,
		"marginType": mgnType,
	}

	body, err := c.PostCtx(ctx, "/openApi/swap/v2/trade/marginType", params, nil)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "marginType")
}

// Helper mapping methods.

func mapOrderTypeAndTif(orderType domain.OrderType) (string, string) {
	switch orderType {
	case exchange.OrderTypeMarket:
		return "MARKET", ""
	case exchange.OrderTypePostOnly:
		return orderTypeLimit, "PO"
	case exchange.OrderTypeIOC:
		return orderTypeLimit, "IOC"
	case exchange.OrderTypeFOK:
		return orderTypeLimit, "FOK"
	default:
		return orderTypeLimit, "GTC"
	}
}

func mapSideAndPosSide(side domain.Side, posMode domain.PositionMode) (string, string) {
	ordSide := sideBuy
	posSide := posSideLong
	isHedge := posMode != domain.PositionModeOneWay

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
		default:
			// SideUnknown or default
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			ordSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			ordSide = sideSell
		default:
			// SideUnknown or default
		}
		posSide = posSideBoth
	}
	return ordSide, posSide
}

func (c *Client) toOrderInfo(o *bingxOrder) *exchange.OrderInfo {
	var state domain.OrderState // default active/pending.
	switch o.Status {
	case stateLive:
		state = exchange.OrderStateNew
	case statePartFill:
		state = exchange.OrderStatePartiallyFilled
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	default:
		state = exchange.OrderStateNew
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
	qtyStr := o.Quantity
	if qtyStr == "" {
		qtyStr = o.OrigQty
	}
	qty := decmath.ParseFloat(qtyStr)
	exec := decmath.ParseFloat(o.ExecutedQty)
	avg := decmath.ParseFloat(o.AvgPrice)

	return &exchange.OrderInfo{
		OrderID:      o.OrderID.String(),
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          qty,
		DealVol:      exec,
		DealAvgPrice: avg,
		State:        state,
		ExternalOID:  o.ClientOid,
		Side:         sideVal,
		CreateTime:   o.Time,
	}
}
