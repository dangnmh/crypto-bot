package bitget

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
	Force                  string `json:"force,omitempty"`
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

type bitgetOpenOrdersRequest struct {
	ProductType string `json:"productType"`
	Symbol      string `json:"symbol,omitempty"`
}

type bitgetOrderDetail struct {
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

type bitgetEntrustedOrder struct {
	OrderID    string `json:"orderId"`
	ClientOid  string `json:"clientOid"`
	Symbol     string `json:"symbol"`
	Size       string `json:"size"`
	Price      string `json:"price"`
	PriceAvg   string `json:"priceAvg"`
	BaseVolume string `json:"baseVolume"`
	Status     string `json:"status"`
	Side       string `json:"side"`
	PosSide    string `json:"posSide"`
	Leverage   string `json:"leverage"`
	CTime      string `json:"cTime"`
	UTime      string `json:"uTime"`
}

type bitgetOrdersResponse struct {
	EntrustedList []bitgetEntrustedOrder `json:"entrustedList"`
	EndID         string                 `json:"endId"`
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

	if req.Force != "" {
		bodyMap["force"] = req.Force
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

// Private raw methods invoking the Bitget REST API.

func (c *Client) getRawOpenOrders(ctx context.Context, req bitgetOpenOrdersRequest) ([]bitgetEntrustedOrder, error) {
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

	res, err := ParseResponse[bitgetOrdersResponse](body, "open_orders")
	if err != nil {
		return nil, err
	}
	return res.EntrustedList, nil
}

func (c *Client) getRawOrderDetail(ctx context.Context, symbol, orderID, clientOid string) (*bitgetOrderDetail, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
		paramSymbol:      symbol,
	}
	if orderID != "" {
		params["orderId"] = orderID
	}
	if clientOid != "" {
		params["clientOid"] = clientOid
	}

	body, err := c.GetCtx(ctx, pathGetOrder, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bitgetOrderDetail](body, "order_detail")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, bodyMap map[string]any) error {
	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "change_leverage")
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

func mapOrderTypeAndForce(t domain.OrderType) (string, string) {
	switch t {
	case exchange.OrderTypeMarket:
		return "market", ""
	case exchange.OrderTypePostOnly:
		return paramLimit, "post_only"
	case exchange.OrderTypeIOC:
		return paramLimit, "ioc"
	case exchange.OrderTypeFOK:
		return paramLimit, "fok"
	default:
		return paramLimit, "gtc"
	}
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	orderType, force := mapOrderTypeAndForce(req.Type)

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
		OrderType:              orderType,
		Force:                  force,
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

// GetOrder queries a single order by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrderDetail(ctx, symbol, orderID, "")
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	info := mapBitgetOrderDetail(*raw)
	return &info, nil
}

// GetOrderByExternalID queries a single order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrderDetail(ctx, symbol, "", externalOrderID)
	if err != nil {
		return nil, err
	}
	if raw == nil {
		return nil, fmt.Errorf("order not found by external ID: %s", externalOrderID)
	}
	info := mapBitgetOrderDetail(*raw)
	return &info, nil
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
		orders = append(orders, mapBitgetEntrustedOrder(rawList[i]))
	}

	return orders, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "bitget"),
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	bodyMap := map[string]any{
		paramSymbol:      symbol,
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.PostCtx(ctx, "/api/v2/mix/order/close-positions", bodyMap)
	if err != nil {
		return err
	}

	var resp struct {
		Code string `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			SuccessList []struct {
				OrderID   string `json:"orderId"`
				ClientOid string `json:"clientOid"`
				Symbol    string `json:"symbol"`
			} `json:"successList"`
			FailureList []struct {
				OrderID   string `json:"orderId"`
				ClientOid string `json:"clientOid"`
				Symbol    string `json:"symbol"`
				ErrorMsg  string `json:"errorMsg"`
				ErrorCode string `json:"errorCode"`
			} `json:"failureList"`
		} `json:"data"`
	}

	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse close positions response: %w", err)
	}

	if resp.Code != "00000" {
		codeVal := 0
		_, _ = fmt.Sscanf(resp.Code, "%d", &codeVal)
		return toAPIError(codeVal, resp.Msg, "close-positions")
	}

	if len(resp.Data.FailureList) > 0 {
		fail := resp.Data.FailureList[0]
		return fmt.Errorf("close position failure for %s: %s (code: %s)", fail.Symbol, fail.ErrorMsg, fail.ErrorCode)
	}

	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	isCross := req.OpenType == exchange.OpenTypeCross

	bodyMap := map[string]any{
		paramSymbol:      req.Symbol,
		paramProductType: productTypeUsdtFutures,
		paramMarginCoin:  constantUsdt,
	}

	if isCross {
		bodyMap[paramLeverage] = strconv.Itoa(req.Leverage)
	} else {
		switch req.PositionType {
		case exchange.PositionTypeLong:
			bodyMap[paramLeverage] = strconv.Itoa(req.Leverage)
			bodyMap["holdSide"] = "long"
		case exchange.PositionTypeShort:
			bodyMap[paramLeverage] = strconv.Itoa(req.Leverage)
			bodyMap["holdSide"] = "short"
		default:
			bodyMap["longLeverage"] = strconv.Itoa(req.Leverage)
			bodyMap["shortLeverage"] = strconv.Itoa(req.Leverage)
		}
	}

	return c.changeRawLeverage(ctx, bodyMap)
}

// SwitchPositionMode switches hold mode between hedge and one-way for Bitget.
func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	posMode := "hedge_mode"
	if positionMode == domain.PositionModeOneWay {
		posMode = "one_way_mode"
	}

	bodyMap := map[string]any{
		"posMode":        posMode,
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.PostCtx(ctx, "/api/v2/mix/account/set-position-mode", bodyMap)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "set-position-mode")
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Bitget.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	mgnMode := modeIsolated
	if marginMode == domain.MarginModeCross {
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

func mapBitgetRawOrder(
	orderID, clientOid, symbol, size, price, priceAvg, baseVolume, side, posSide, leverage, cTime, uTime, statusOrState string,
) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(price, 64)
	sz, _ := strconv.ParseFloat(size, 64)
	avgPx, _ := strconv.ParseFloat(priceAvg, 64)
	fillSz, _ := strconv.ParseFloat(baseVolume, 64)

	cTimeVal := decmath.ParseInt64(cTime)
	uTimeVal := decmath.ParseInt64(uTime)

	info := exchange.OrderInfo{
		OrderID:      orderID,
		Symbol:       symbol,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  clientOid,
		CreateTime:   cTimeVal,
		UpdateTime:   uTimeVal,
		PositionMode: domain.PositionModeOneWay, // default OneWay
	}

	switch posSide {
	case posSideLong:
		info.PositionMode = domain.PositionModeHedge
		if side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideCloseLong
		}
	case posSideShort:
		info.PositionMode = domain.PositionModeHedge
		if side == sideSell {
			info.Side = exchange.SideOpenShort
		} else {
			info.Side = exchange.SideCloseShort
		}
	default:
		if side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	switch statusOrState {
	case stateInit, stateLive:
		info.State = exchange.OrderStateNew
	case statePartFill:
		info.State = exchange.OrderStatePartiallyFilled
	case stateFilled:
		info.State = exchange.OrderStateFilled
	case stateCanceled:
		info.State = exchange.OrderStateCanceled
	default:
		info.State = exchange.OrderStateNew
	}

	return info
}

func mapBitgetOrderDetail(o bitgetOrderDetail) exchange.OrderInfo {
	return mapBitgetRawOrder(
		o.OrderID, o.ClientOid, o.Symbol, o.Size, o.Price, o.PriceAvg, o.BaseVolume,
		o.Side, o.PosSide, o.Leverage, o.CTime, o.UTime, o.State,
	)
}

func mapBitgetEntrustedOrder(o bitgetEntrustedOrder) exchange.OrderInfo {
	return mapBitgetRawOrder(
		o.OrderID, o.ClientOid, o.Symbol, o.Size, o.Price, o.PriceAvg, o.BaseVolume,
		o.Side, o.PosSide, o.Leverage, o.CTime, o.UTime, o.Status,
	)
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
