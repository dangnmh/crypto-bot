package bitget

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bitgetCreateOrderRequest struct {
	Symbol                 string `json:"symbol"`
	ProductType            string `json:"productType"`
	MarginMode             string `json:"marginMode"`
	Side                   string `json:"side"`
	OrderType              string `json:"orderType"`
	Size                   string `json:"size"`
	MarginCoin             string `json:"marginCoin"`
	TradeSide              string `json:"tradeSide,omitempty"`
	Price                  string `json:"price,omitempty"`
	ClientOid              string `json:"clientOid,omitempty"`
	ReduceOnly             string `json:"reduceOnly,omitempty"`
	PresetStopSurplusPrice string `json:"presetStopSurplusPrice,omitempty"`
	PresetStopLossPrice    string `json:"presetStopLossPrice,omitempty"`
}

type bitgetCreateOrderResponse struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type bitgetCancelOrderRequest struct {
	Symbol      string `json:"symbol"`
	ProductType string `json:"productType"`
	OrderID     string `json:"orderId"`
}

type bitgetCancelOrderResponse struct{}

type bitgetPendingOrdersRequest struct {
	ProductType string `json:"productType"`
}

type bitgetOpenOrdersRequest struct {
	ProductType string `json:"productType"`
	Symbol      string `json:"symbol,omitempty"`
}

type bitgetHistoryOrdersRequest struct {
	ProductType string `json:"productType"`
}

type bitgetChangeLeverageRequest struct {
	Symbol        string `json:"symbol"`
	ProductType   string `json:"productType"`
	MarginCoin    string `json:"marginCoin"`
	Leverage      string `json:"leverage"`
	LongLeverage  string `json:"longLeverage"`
	ShortLeverage string `json:"shortLeverage"`
}

type bitgetChangeLeverageResponse struct{}

type bitgetOrder struct {
	OrderID    string `json:"orderId"`
	ClientOid  string `json:"clientOid"`
	Symbol     string `json:"symbol"`
	Size       string `json:"size"`
	Price      string `json:"price"`
	PriceAvg   string `json:"priceAvg"`
	BaseVolume string `json:"baseVolume"`
	State      string `json:"state"`
	Side       string `json:"side"`
	PosSide    string `json:"posSide"`
	Leverage   string `json:"leverage"`
	CTime      string `json:"cTime"`
	UTime      string `json:"uTime"`
}

// Private raw methods invoking the Bitget REST API.

func (c *Client) createRawOrder(ctx context.Context, req bitgetCreateOrderRequest) (*bitgetCreateOrderResponse, error) {
	bodyMap := map[string]any{
		paramSymbol:      req.Symbol,
		paramProductType: req.ProductType,
		paramMarginMode:  req.MarginMode,
		"side":           req.Side,
		"orderType":      req.OrderType,
		"size":           req.Size,
		paramMarginCoin:  req.MarginCoin,
	}

	if req.TradeSide != "" {
		bodyMap["tradeSide"] = req.TradeSide
	}

	if req.Price != "" {
		bodyMap["price"] = req.Price
	}

	if req.ClientOid != "" {
		bodyMap["clientOid"] = req.ClientOid
	}

	if req.ReduceOnly != "" {
		bodyMap["reduceOnly"] = req.ReduceOnly
	}

	if req.PresetStopSurplusPrice != "" {
		bodyMap["presetStopSurplusPrice"] = req.PresetStopSurplusPrice
	}

	if req.PresetStopLossPrice != "" {
		bodyMap["presetStopLossPrice"] = req.PresetStopLossPrice
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bitgetCreateOrderResponse](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req bitgetCancelOrderRequest) (*bitgetCancelOrderResponse, error) {
	bodyMap := map[string]any{
		paramSymbol:      req.Symbol,
		paramProductType: req.ProductType,
		"orderId":        req.OrderID,
	}

	body, err := c.PostCtx(ctx, pathCancelOrder, bodyMap)
	if err != nil {
		return nil, err
	}

	if err := ParseResponseIgnoreData(body, "cancel_order"); err != nil {
		return nil, err
	}
	return &bitgetCancelOrderResponse{}, nil
}

func (c *Client) getRawPendingOrders(ctx context.Context, req bitgetPendingOrdersRequest) ([]bitgetOrder, error) {
	pendingBody, err := c.GetCtx(ctx, pathPendingOrders, map[string]string{paramProductType: req.ProductType})
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]bitgetOrder](pendingBody, "pending_orders")
}

func (c *Client) getRawHistoryOrders(ctx context.Context, req bitgetHistoryOrdersRequest) ([]bitgetOrder, error) {
	historyBody, err := c.GetCtx(ctx, "/api/v2/mix/order/orders-history", map[string]string{paramProductType: req.ProductType})
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]bitgetOrder](historyBody, "orders_history")
}

func (c *Client) getRawOpenOrders(ctx context.Context, req bitgetOpenOrdersRequest) ([]bitgetOrder, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bitgetOrder](body, "open_orders")
}

func (c *Client) changeRawLeverage(ctx context.Context, req bitgetChangeLeverageRequest) (*bitgetChangeLeverageResponse, error) {
	bodyMap := map[string]any{
		paramSymbol:      req.Symbol,
		paramProductType: req.ProductType,
		paramMarginCoin:  req.MarginCoin,
		paramLeverage:    req.Leverage,
		"longLeverage":   req.LongLeverage,
		"shortLeverage":  req.ShortLeverage,
	}

	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return nil, err
	}

	if err := ParseResponseIgnoreData(body, "change_leverage"); err != nil {
		return nil, err
	}
	return &bitgetChangeLeverageResponse{}, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType := mapBitgetOrderType(req.Type)
	isHedge := req.PositionMode == 1 || req.PositionMode == 0
	side, tradeSide := mapBitgetOrderSide(req.Side, isHedge)

	marginMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		marginMode = modeCrossed
	}

	reduceOnlyVal := "NO"
	if req.ReduceOnly {
		reduceOnlyVal = "YES"
	}

	var presetStopSurplusPrice string
	if req.TakeProfitPrice > 0 {
		presetStopSurplusPrice = decmath.FormatFloat(req.TakeProfitPrice)
	}

	var presetStopLossPrice string
	if req.StopLossPrice > 0 {
		presetStopLossPrice = decmath.FormatFloat(req.StopLossPrice)
	}

	rawReq := bitgetCreateOrderRequest{
		Symbol:                 req.Symbol,
		ProductType:            productTypeUsdtFutures,
		MarginMode:             marginMode,
		Side:                   side,
		OrderType:              ordType,
		Size:                   decmath.FormatFloat(req.Vol),
		MarginCoin:             constantUsdt,
		ClientOid:              req.ExternalOID,
		ReduceOnly:             reduceOnlyVal,
		PresetStopSurplusPrice: presetStopSurplusPrice,
		PresetStopLossPrice:    presetStopLossPrice,
	}

	if isHedge {
		rawReq.TradeSide = tradeSide
	}

	if req.Type != exchange.OrderTypeMarket {
		rawReq.Price = decmath.FormatFloat(req.Price)
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0

	if res.OrderID == "" && req.ExternalOID != "" {
		return exchange.CreateOrderResult{
			OrderID:       req.ExternalOID,
			TPSLSubmitted: tpslSubmitted,
		}, nil
	}

	return exchange.CreateOrderResult{
		OrderID:       res.OrderID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

// CreateTrackOrder submits a trailing stop order (stubbed/not implemented).
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Bitget")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if symbol == "" {
		info, err := c.GetOrder(ctx, "", orderID)
		if err != nil {
			return fmt.Errorf("cancel order failed to locate order: %w", err)
		}
		symbol = info.Symbol
	}

	req := bitgetCancelOrderRequest{
		Symbol:      symbol,
		ProductType: productTypeUsdtFutures,
		OrderID:     orderID,
	}

	_, err := c.cancelRawOrder(ctx, req)
	return err
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
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	// Query pending orders.
	pendingList, err := c.getRawPendingOrders(ctx, bitgetPendingOrdersRequest{
		ProductType: productTypeUsdtFutures,
	})
	if err == nil {
		for i := range pendingList {
			o := pendingList[i]
			if o.OrderID == orderID || o.ClientOid == orderID {
				info := mapBitgetOrder(o)
				return &info, nil
			}
		}
	}

	// Historical order detail.
	historyList, err := c.getRawHistoryOrders(ctx, bitgetHistoryOrdersRequest{
		ProductType: productTypeUsdtFutures,
	})
	if err == nil {
		for i := range historyList {
			o := historyList[i]
			if o.OrderID == orderID || o.ClientOid == orderID {
				info := mapBitgetOrder(o)
				return &info, nil
			}
		}
	}

	return nil, fmt.Errorf("order not found: %s", orderID)
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.getRawOpenOrders(ctx, bitgetOpenOrdersRequest{
		ProductType: productTypeUsdtFutures,
		Symbol:      symbol,
	})
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		orders = append(orders, mapBitgetOrder(rawList[i]))
	}

	return orders, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
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
			if pos.PositionType == exchange.PositionTypeLong { // Long
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
	rawReq := bitgetChangeLeverageRequest{
		Symbol:        req.Symbol,
		ProductType:   productTypeUsdtFutures,
		MarginCoin:    constantUsdt,
		Leverage:      strconv.Itoa(req.Leverage),
		LongLeverage:  strconv.Itoa(req.Leverage),
		ShortLeverage: strconv.Itoa(req.Leverage),
	}

	_, err := c.changeRawLeverage(ctx, rawReq)
	return err
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Bitget.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	mgnMode := modeIsolated
	if marginMode == "CROSS" {
		mgnMode = modeCrossed
	}

	bodyMap := map[string]any{
		"symbol":      symbol,
		"productType": productTypeUsdtFutures,
		"marginCoin":  constantUsdt,
		"marginMode":  mgnMode,
	}

	body, err := c.PostCtx(ctx, "/api/v2/mix/account/set-margin-mode", bodyMap)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "set-margin-mode")
}

func mapBitgetOrder(o bitgetOrder) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(o.Price, 64)
	sz, _ := strconv.ParseFloat(o.Size, 64)
	avgPx, _ := strconv.ParseFloat(o.PriceAvg, 64)
	fillSz, _ := strconv.ParseFloat(o.BaseVolume, 64)

	cTimeVal := decmath.ParseInt64(o.CTime)
	uTimeVal := decmath.ParseInt64(o.UTime)

	info := exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClientOid,
		CreateTime:   cTimeVal,
		UpdateTime:   uTimeVal,
		PositionMode: domain.PositionModeOneWay, // default OneWay
	}

	switch o.PosSide {
	case posSideLong:
		info.PositionMode = domain.PositionModeHedge
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideCloseLong
		}
	case posSideShort:
		info.PositionMode = domain.PositionModeHedge
		if o.Side == sideSell {
			info.Side = exchange.SideOpenShort
		} else {
			info.Side = exchange.SideCloseShort
		}
	default:
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

func mapBitgetOrderType(t domain.OrderType) string {
	switch t {
	case exchange.OrderTypeMarket:
		return "market"
	case exchange.OrderTypePostOnly:
		return "post_only"
	case exchange.OrderTypeIOC:
		return "ioc"
	case exchange.OrderTypeFOK:
		return "fok"
	default:
		return paramLimit
	}
}

func mapBitgetOrderSide(s domain.Side, isHedge bool) (string, string) {
	if isHedge {
		switch s {
		case exchange.SideOpenLong:
			return sideBuy, sideOpen
		case exchange.SideCloseLong:
			return sideSell, sideClose
		case exchange.SideOpenShort:
			return sideSell, sideOpen
		case exchange.SideCloseShort:
			return sideBuy, sideClose
		default:
			return sideBuy, sideOpen
		}
	}

	switch s {
	case exchange.SideOpenLong, exchange.SideCloseShort:
		return sideBuy, sideOpen
	case exchange.SideOpenShort, exchange.SideCloseLong:
		return sideSell, sideOpen
	default:
		return sideBuy, sideOpen
	}
}
