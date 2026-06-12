package bybit

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for order endpoints.

type bybitCreateOrderRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	OrderType   string `json:"orderType"`
	Qty         string `json:"qty"`
	Price       string `json:"price,omitempty"`
	TimeInForce string `json:"timeInForce,omitempty"`
	PositionIdx int    `json:"positionIdx"`
	ReduceOnly  bool   `json:"reduceOnly,omitempty"`
	OrderLinkID string `json:"orderLinkId,omitempty"`
	TakeProfit  string `json:"takeProfit,omitempty"`
	StopLoss    string `json:"stopLoss,omitempty"`
	Leverage    string `json:"leverage,omitempty"`
}

type bybitPlaceTPSLRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	TakeProfit  string `json:"takeProfit,omitempty"`
	StopLoss    string `json:"stopLoss,omitempty"`
	TpTriggerBy string `json:"tpTriggerBy,omitempty"`
	SlTriggerBy string `json:"slTriggerBy,omitempty"`
	PositionIdx int    `json:"positionIdx"`
}

type bybitCancelOrderRequest struct {
	Category    string `json:"category"`
	Symbol      string `json:"symbol"`
	OrderID     string `json:"orderId,omitempty"`
	OrderLinkID string `json:"orderLinkId,omitempty"`
}

type bybitCancelAllOpenOrdersRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol"`
}

type bybitGetOrderRequest struct {
	Category    string `json:"category"`
	OrderID     string `json:"orderId,omitempty"`
	OrderLinkID string `json:"orderLinkId,omitempty"`
	Symbol      string `json:"symbol,omitempty"`
}

type bybitListOpenOrdersRequest struct {
	Category string `json:"category"`
	Symbol   string `json:"symbol,omitempty"`
}

type bybitChangeLeverageRequest struct {
	Category     string `json:"category"`
	Symbol       string `json:"symbol"`
	BuyLeverage  string `json:"buyLeverage"`
	SellLeverage string `json:"sellLeverage"`
}

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

// Private raw methods invoking the Bybit API.

func (c *Client) createRawOrder(ctx context.Context, req bybitCreateOrderRequest) (*bybitCreateOrderResult, error) {
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/order/create", req, true)
	if err != nil {
		return nil, fmt.Errorf("bybit create order: %w", err)
	}
	res, err := parseResponse[bybitCreateOrderResult](body, "bybit create order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) placeRawTPSL(ctx context.Context, req bybitPlaceTPSLRequest) error {
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/position/trading-stop", req, true)
	if err != nil {
		return fmt.Errorf("bybit set trading stop: %w", err)
	}
	_, err = parseResponse[any](body, "bybit set trading stop")
	return err
}

func (c *Client) cancelRawOrder(ctx context.Context, req bybitCancelOrderRequest) error {
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/order/cancel", req, true)
	if err != nil {
		return fmt.Errorf("bybit cancel order: %w", err)
	}
	var resp bybitResponse[any]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit cancel order json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110001 || strings.Contains(strings.ToLower(resp.RetMsg), "already cancelled") || strings.Contains(strings.ToLower(resp.RetMsg), "filled") {
			return nil
		}
		return fmt.Errorf("bybit cancel order error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, req bybitCancelAllOpenOrdersRequest) error {
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/order/cancel-all", req, true)
	if err != nil {
		return fmt.Errorf("bybit cancel all orders: %w", err)
	}
	_, err = parseResponse[any](body, "bybit cancel all orders")
	return err
}

func (c *Client) getRawOrder(ctx context.Context, req bybitGetOrderRequest) (*bybitOrder, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/order/realtime", req, true)
	if err != nil {
		return nil, err
	}
	list, err := decodeListResponse[bybitOrder](body, "bybit get order")
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		id := req.OrderID
		if id == "" {
			id = req.OrderLinkID
		}
		return nil, fmt.Errorf("bybit order not found: %s", id)
	}
	return &list[0], nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req bybitListOpenOrdersRequest) ([]bybitOrder, error) {
	body, err := c.sendRequest(ctx, http.MethodGet, "/v5/order/realtime", req, true)
	if err != nil {
		return nil, err
	}
	return decodeListResponse[bybitOrder](body, "bybit list open orders")
}

func (c *Client) changeRawLeverage(ctx context.Context, req bybitChangeLeverageRequest) error {
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/position/set-leverage", req, true)
	if err != nil {
		return fmt.Errorf("bybit change leverage: %w", err)
	}
	var resp bybitResponse[any]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit change leverage json unmarshal: %w", err)
	}
	if resp.RetCode != 0 && resp.RetCode != 110043 {
		return fmt.Errorf("bybit change leverage error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	bybitOrderType, bybitTif := mapOrderTypeAndTif(req.Type)
	bybitSide, positionIdx, reduceOnly := mapSideAndPosition(req.Side, req.PositionMode == 1)

	rawReq := bybitCreateOrderRequest{
		Category:    categoryLinear,
		Symbol:      req.Symbol,
		Side:        bybitSide,
		OrderType:   bybitOrderType,
		Qty:         decmath.FormatFloat(req.Vol),
		TimeInForce: bybitTif,
		PositionIdx: positionIdx,
	}

	if req.Type != exchange.OrderTypeMarket {
		rawReq.Price = decmath.FormatFloat(req.Price)
	}
	if reduceOnly || req.ReduceOnly {
		rawReq.ReduceOnly = true
	}
	if req.ExternalOID != "" {
		rawReq.OrderLinkID = req.ExternalOID
	}
	if req.TakeProfitPrice > 0 {
		rawReq.TakeProfit = decmath.FormatFloat(req.TakeProfitPrice)
	}
	if req.StopLossPrice > 0 {
		rawReq.StopLoss = decmath.FormatFloat(req.StopLossPrice)
	}
	if req.Leverage > 0 {
		rawReq.Leverage = fmt.Sprintf("%d", req.Leverage)
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{OrderID: res.OrderID, TPSLSubmitted: tpslSubmitted}, nil
}

// PlaceTPSL places Take Profit and Stop Loss on Bybit.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	rawReq := bybitPlaceTPSLRequest{
		Category: categoryLinear,
		Symbol:   req.Symbol,
	}

	if req.TakeProfitPrice > 0 {
		rawReq.TakeProfit = decmath.FormatFloat(req.TakeProfitPrice)
		rawReq.TpTriggerBy = triggerByLastPrice
	}
	if req.StopLossPrice > 0 {
		rawReq.StopLoss = decmath.FormatFloat(req.StopLossPrice)
		rawReq.SlTriggerBy = triggerByLastPrice
	}

	// positionIdx: 0=OneWay, 1=Hedge Long, 2=Hedge Short.
	positionIdx := 0
	if req.PositionMode == 1 {
		switch req.Side {
		case exchange.SideOpenLong:
			positionIdx = 1
		case exchange.SideOpenShort:
			positionIdx = 2
		default:
			// SideUnknown or default
		}
	}
	rawReq.PositionIdx = positionIdx

	return c.placeRawTPSL(ctx, rawReq)
}

// CreateTrackOrder submits a trailing stop order. Stubbed since track orders are not used in Core reversion.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Bybit")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return c.cancelRawOrder(ctx, bybitCancelOrderRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		OrderID:  orderID,
	})
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		err := c.CancelOrder(ctx, "", id)
		if err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return c.cancelRawAllOpenOrders(ctx, bybitCancelAllOpenOrdersRequest{
		Category: categoryLinear,
		Symbol:   symbol,
	})
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrder(ctx, bybitGetOrderRequest{
		Category: categoryLinear,
		Symbol:   symbol,
		OrderID:  orderID,
	})
	if err != nil {
		return nil, err
	}
	info := mapOrderInfo(*raw)
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.getRawOpenOrders(ctx, bybitListOpenOrdersRequest{
		Category: categoryLinear,
		Symbol:   symbol,
	})
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		orders = append(orders, mapOrderInfo(rawList[i]))
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
		pos := &positions[i]
		if pos.HoldVol > 0 {
			var side domain.Side
			if pos.PositionType == exchange.PositionTypeLong { // Long
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
	leverageStr := fmt.Sprintf("%d", req.Leverage)
	return c.changeRawLeverage(ctx, bybitChangeLeverageRequest{
		Category:     categoryLinear,
		Symbol:       req.Symbol,
		BuyLeverage:  leverageStr,
		SellLeverage: leverageStr,
	})
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Bybit.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	if strings.EqualFold(c.accountType, "unified") {
		return c.switchUnifiedMarginMode(ctx, marginMode)
	}

	tradeMode := 1 // isolated
	if marginMode == constantCross {
		tradeMode = 0 // cross
	}
	leverageStr := fmt.Sprintf("%d", leverage)
	params := map[string]any{
		"category":     categoryLinear,
		"symbol":       symbol,
		"tradeMode":    tradeMode,
		"buyLeverage":  leverageStr,
		"sellLeverage": leverageStr,
	}
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/position/switch-isolated", params, true)
	if err != nil {
		return fmt.Errorf("bybit switch margin mode: %w", err)
	}
	var resp bybitResponse[any]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit switch margin mode json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110026 || strings.Contains(strings.ToLower(resp.RetMsg), "already") {
			return nil
		}
		// Fallback for unified account
		if resp.RetCode == 100028 || strings.Contains(strings.ToLower(resp.RetMsg), "unified account is forbidden") {
			c.logger.InfoContext(ctx, "Bybit SwitchPositionMargin returned unified account restriction, falling back to SetMarginMode", slog.String("symbol", symbol), slog.String("marginMode", marginMode))
			return c.switchUnifiedMarginMode(ctx, marginMode)
		}
		return fmt.Errorf("bybit switch margin mode error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

func (c *Client) switchUnifiedMarginMode(ctx context.Context, marginMode string) error {
	utaMarginMode := utaMarginIsolated
	if marginMode == constantCross {
		utaMarginMode = utaMarginRegular
	}
	utaParams := map[string]any{
		paramSetMarginMode: utaMarginMode,
	}
	body, err := c.sendRequest(ctx, http.MethodPost, "/v5/account/set-margin-mode", utaParams, true)
	if err != nil {
		return fmt.Errorf("bybit set account margin mode: %w", err)
	}
	var resp bybitResponse[any]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit set account margin mode json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110026 || strings.Contains(strings.ToLower(resp.RetMsg), "already") {
			return nil
		}
		return fmt.Errorf("bybit set account margin mode error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// Helper mapping functions.

func mapOrderTypeAndTif(orderType domain.OrderType) (string, string) {
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

func mapSideAndPosition(reqSide domain.Side, isHedge bool) (string, int, bool) {
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
		default:
			// SideUnknown or default
		}
	} else {
		positionIdx = 0
		switch reqSide {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			bybitSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			bybitSide = sideSell
		default:
			// SideUnknown or default
		}
	}
	return bybitSide, positionIdx, reduceOnly
}

// mapBybitSide maps raw Bybit side and position index to domain Side.
func mapBybitSide(side string, positionIdx int) domain.Side {
	switch side {
	case sideBuy:
		if positionIdx == 2 {
			return exchange.SideCloseShort
		}
		return exchange.SideOpenLong
	case sideSell:
		if positionIdx == 1 {
			return exchange.SideCloseLong
		}
		return exchange.SideOpenShort
	default:
		return domain.SideUnknown
	}
}

// mapBybitStatus maps raw Bybit order status to domain OrderState.
func mapBybitStatus(status string) domain.OrderState {
	switch strings.ToLower(status) {
	case "new":
		return exchange.OrderStateNew
	case "partiallyfilled":
		return exchange.OrderStatePartiallyFilled
	case "filled":
		return exchange.OrderStateFilled
	case "cancelled", "rejected":
		return exchange.OrderStateCanceled
	case "untriggered":
		return exchange.OrderStateUntriggered
	default:
		return exchange.OrderStateNew
	}
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
		PositionMode: domain.PositionModeOneWay, // Default One-Way.
	}

	if raw.PositionIdx > 0 {
		info.PositionMode = domain.PositionModeHedge // Hedge mode.
	}

	// Created/Updated times.
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

	info.Side = mapBybitSide(raw.Side, raw.PositionIdx)
	info.State = mapBybitStatus(raw.OrderStatus)

	return info
}

// structToMap converts any struct to a map[string]any.
func structToMap(val any) map[string]any {
	data, err := json.Marshal(val)
	if err != nil {
		return nil
	}
	var res map[string]any
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	if err := decoder.Decode(&res); err != nil {
		return nil
	}
	return res
}
