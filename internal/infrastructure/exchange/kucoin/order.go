package kucoin

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/google/uuid"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

type kucoinCreateOrderRequest struct {
	ClientOid            string  `json:"clientOid"`
	Side                 string  `json:"side"`
	Symbol               string  `json:"symbol"`
	Leverage             int     `json:"leverage,omitempty"`
	Type                 string  `json:"type,omitempty"`
	Remark               string  `json:"remark,omitempty"`
	Stop                 string  `json:"stop,omitempty"`
	StopPriceType        string  `json:"stopPriceType,omitempty"`
	StopPrice            string  `json:"stopPrice,omitempty"`
	ReduceOnly           bool    `json:"reduceOnly,omitempty"`
	CloseOrder           bool    `json:"closeOrder,omitempty"`
	ForceHold            bool    `json:"forceHold,omitempty"`
	Stp                  string  `json:"stp,omitempty"`
	MarginMode           string  `json:"marginMode,omitempty"`
	Price                string  `json:"price,omitempty"`
	Size                 float64 `json:"size,omitempty"`
	Qty                  string  `json:"qty,omitempty"`
	ValueQty             string  `json:"valueQty,omitempty"`
	TimeInForce          string  `json:"timeInForce,omitempty"`
	PostOnly             bool    `json:"postOnly,omitempty"`
	Hidden               bool    `json:"hidden,omitempty"`
	Iceberg              bool    `json:"iceberg,omitempty"`
	VisibleSize          string  `json:"visibleSize,omitempty"`
	PositionSide         string  `json:"positionSide,omitempty"`
	TriggerStopUpPrice   string  `json:"triggerStopUpPrice,omitempty"`
	TriggerStopDownPrice string  `json:"triggerStopDownPrice,omitempty"`
}

type kucoinCreateOrderResponse struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type kucoinCancelOrderRequest struct {
	OrderID string `json:"orderId"`
}

type kucoinCancelOrderResponse struct{}

type kucoinOrderRequest struct {
	OrderID string `json:"orderId"`
}

type kucoinOpenOrdersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type kucoinOrder struct {
	OrderID      string `json:"id"`
	ClientOid    string `json:"clientOid"`
	Symbol       string `json:"symbol"`
	Side         string `json:"side"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	Price        string `json:"price"`
	Status       string `json:"status"`
	DealSize     int64  `json:"dealSize"`
	StatusVal    string `json:"statusVal"`
	CreatedAt    int64  `json:"createdAt"`
	FilledValue  string `json:"filledValue"`
	AvgDealPrice string `json:"avgDealPrice"`
	CancelExist  bool   `json:"cancelExist"`
	IsActive     bool   `json:"isActive"`
}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) createRawOrder(ctx context.Context, req kucoinCreateOrderRequest) (*kucoinCreateOrderResponse, error) {
	path := pathPlaceOrder
	if req.TriggerStopUpPrice != "" || req.TriggerStopDownPrice != "" {
		path = "/api/v1/st-orders"
	}
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("kucoin marshal create order request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, path, nil, bodyBytes)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[kucoinCreateOrderResponse](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req kucoinCancelOrderRequest) (*kucoinCancelOrderResponse, error) {
	path := fmt.Sprintf("%s/%s", pathCancelOrder, req.OrderID)
	body, err := c.RawRequest(ctx, http.MethodDelete, path, nil, nil)
	if err != nil {
		return nil, err
	}

	if err := ParseResponseIgnoreData(body, "cancel_order"); err != nil {
		return nil, err
	}
	return &kucoinCancelOrderResponse{}, nil
}

func (c *Client) getRawOrder(ctx context.Context, req kucoinOrderRequest) (*kucoinOrder, error) {
	body, err := c.GetOrderDetailRaw(ctx, req.OrderID, nil)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[kucoinOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req kucoinOpenOrdersRequest) ([]kucoinOrder, error) {
	params := map[string]string{
		paramStatus: stateLive,
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v1/orders", params, nil)
	if err != nil {
		return nil, err
	}

	type orderListData struct {
		Items []kucoinOrder `json:"items"`
	}

	var rawList []kucoinOrder
	listParsed, err := ParseResponse[orderListData](body, "open_orders")
	if err == nil {
		rawList = listParsed.Items
	} else {
		directParsed, err := ParseResponse[[]kucoinOrder](body, "open_orders")
		if err == nil {
			rawList = directParsed
		} else {
			return nil, fmt.Errorf("parse open orders failed: %w", err)
		}
	}

	return rawList, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType, timeInForce, postOnly := mapOrderType(req.Type)
	side, posSide, reduceOnly := mapSideAndPosition(req.Side, req.PositionMode == 1)

	if req.ReduceOnly {
		reduceOnly = true
	}

	clientOid := req.ExternalOID
	if clientOid == "" {
		clientOid = uuid.NewString()
	}

	marginMode := "ISOLATED"
	if req.OpenType == exchange.OpenTypeCross {
		marginMode = "CROSS"
	}

	rawReq := kucoinCreateOrderRequest{
		Symbol:       req.Symbol,
		Side:         side,
		Type:         ordType,
		Size:         req.Vol,
		ClientOid:    clientOid,
		ReduceOnly:   reduceOnly,
		PositionSide: posSide,
		MarginMode:   marginMode,
	}

	if ordType != constantMarket {
		rawReq.Price = decmath.FormatFloat(req.Price)
	}
	if req.Leverage > 0 {
		rawReq.Leverage = req.Leverage
	}
	if timeInForce != "" {
		rawReq.TimeInForce = timeInForce
	}
	if postOnly {
		rawReq.PostOnly = true
	}

	var stopUpPrice, stopDownPrice float64
	if req.Side == exchange.SideOpenLong {
		stopUpPrice = req.TakeProfitPrice
		stopDownPrice = req.StopLossPrice
	} else {
		stopUpPrice = req.StopLossPrice
		stopDownPrice = req.TakeProfitPrice
	}

	if stopUpPrice > 0 {
		rawReq.TriggerStopUpPrice = decmath.FormatFloat(stopUpPrice)
	}
	if stopDownPrice > 0 {
		rawReq.TriggerStopDownPrice = decmath.FormatFloat(stopDownPrice)
	}
	if req.TakeProfitPrice > 0 || req.StopLossPrice > 0 {
		rawReq.StopPriceType = "TP"
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{
		OrderID:       res.OrderID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

func mapSideAndPosition(reqSide domain.Side, isHedge bool) (string, string, bool) {
	var side string
	var posSide string
	var reduceOnly bool

	if isHedge {
		switch reqSide {
		case exchange.SideOpenLong:
			side = sideBuy
			posSide = constantLong
		case exchange.SideCloseLong:
			side = sideSell
			posSide = constantLong
			reduceOnly = true
		case exchange.SideOpenShort:
			side = sideSell
			posSide = constantShort
		case exchange.SideCloseShort:
			side = sideBuy
			posSide = constantShort
			reduceOnly = true
		default:
			side = sideBuy
			posSide = constantLong
		}
	} else {
		posSide = constantBoth
		switch reqSide {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			side = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			side = sideSell
		default:
			// SideUnknown or unhandled
		}
		if reqSide == exchange.SideCloseLong || reqSide == exchange.SideCloseShort {
			reduceOnly = true
		}
	}
	return side, posSide, reduceOnly
}

func mapOrderType(t domain.OrderType) (string, string, bool) {
	ordType := paramLimit
	var timeInForce string
	var postOnly bool

	switch t {
	case exchange.OrderTypeMarket:
		ordType = constantMarket
	case exchange.OrderTypePostOnly:
		postOnly = true
	case exchange.OrderTypeIOC:
		timeInForce = "IOC"
	case exchange.OrderTypeFOK:
		timeInForce = "GTC"
	default:
		// OrderTypeLimit or default
	}
	return ordType, timeInForce, postOnly
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	_, err := c.cancelRawOrder(ctx, kucoinCancelOrderRequest{
		OrderID: orderID,
	})
	return err
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch CancelOrders not implemented on KuCoin")
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
	raw, err := c.getRawOrder(ctx, kucoinOrderRequest{
		OrderID: orderID,
	})
	if err != nil {
		return nil, err
	}

	return c.toOrderInfo(raw), nil
}

// GetOrderByExternalID fetches details of a specific order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrderByClientOid(ctx, externalOrderID)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfo(raw), nil
}

// GetOpenOrders returns all currently active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.getRawOpenOrders(ctx, kucoinOpenOrdersRequest{
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
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "kucoin"),
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
		if pos.PositionType == exchange.PositionTypeShort { // Short
			closeSide = domain.SideCloseShort
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, domain.PositionModeHedge, pos.Leverage)
	}

	return nil
}

// PlaceTPSL places Take Profit and Stop Loss conditional orders on KuCoin.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	var stopUpPrice, stopDownPrice float64
	if req.Side == exchange.SideOpenLong {
		stopUpPrice = req.TakeProfitPrice
		stopDownPrice = req.StopLossPrice
	} else {
		stopUpPrice = req.StopLossPrice
		stopDownPrice = req.TakeProfitPrice
	}

	bodyMap := map[string]any{
		constantClientOid: "tpsl-" + req.Symbol + "-" + strconv.FormatInt(time.Now().UnixNano(), 10),
		paramSymbol:       req.Symbol,
		"type":            constantMarket,
		"reduceOnly":      true,
		"stopPriceType":   "TP",
		constantSize:      req.Volume,
	}

	if req.PositionMode == 1 { // Hedge Mode
		if req.Side == exchange.SideOpenLong {
			bodyMap["positionSide"] = constantLong
		} else {
			bodyMap["positionSide"] = constantShort
		}
	} else {
		bodyMap["positionSide"] = constantBoth
	}

	if req.Side == exchange.SideOpenLong {
		bodyMap["side"] = sideSell
	} else {
		bodyMap["side"] = sideBuy
	}

	if stopUpPrice > 0 {
		bodyMap["triggerStopUpPrice"] = decmath.FormatFloat(stopUpPrice)
	}
	if stopDownPrice > 0 {
		bodyMap["triggerStopDownPrice"] = decmath.FormatFloat(stopDownPrice)
	}

	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("kucoin marshal place tpsl request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/api/v1/st-orders", nil, bodyBytes)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "place_tpsl")
}

func (c *Client) toOrderInfo(o *kucoinOrder) *exchange.OrderInfo {
	var state domain.OrderState
	if o.IsActive {
		if o.DealSize > 0 {
			state = exchange.OrderStatePartiallyFilled
		} else {
			state = exchange.OrderStateNew
		}
	} else {
		switch {
		case o.CancelExist:
			state = exchange.OrderStateCanceled
		case o.Status == stateFilled || o.StatusVal == stateFilled:
			state = exchange.OrderStateFilled
		default:
			state = exchange.OrderStateCanceled
		}
	}

	sideVal := exchange.SideOpenLong
	if o.Side == sideSell {
		sideVal = exchange.SideOpenShort
	}

	price := decmath.ParseFloat(o.Price)
	qty := float64(o.Size)
	exec := float64(o.DealSize)

	avg := decmath.ParseFloat(o.AvgDealPrice)
	if avg == 0 && exec > 0 {
		val := decmath.ParseFloat(o.FilledValue)
		avg = val / exec
	}

	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          qty,
		DealVol:      exec,
		DealAvgPrice: avg,
		State:        state,
		Side:         sideVal,
		CreateTime:   o.CreatedAt,
	}
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return errors.New("not implemented")
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for KuCoin.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	bodyMap := map[string]any{
		paramSymbol:  symbol,
		"marginMode": marginMode,
	}
	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return fmt.Errorf("kucoin marshal switch margin mode request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/api/v2/position/changeMarginMode", nil, bodyBytes)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "changeMarginMode")
}
