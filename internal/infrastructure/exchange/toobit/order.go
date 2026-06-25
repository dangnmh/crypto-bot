package toobit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type toobitResponse[T any] struct {
	Code    any    `json:"code"`
	Message string `json:"msg"`
	Data    T      `json:"data"`
}

func getResponseCodeStr(envelopeCode any) string {
	switch v := envelopeCode.(type) {
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func parseResponse[T any](body []byte) (T, error) {
	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(trimmed, "[") {
		var data T
		if err := json.Unmarshal(body, &data); err != nil {
			var zero T
			return zero, fmt.Errorf("unmarshal raw array: %w", err)
		}
		return data, nil
	}

	var rawMap map[string]any
	if err := json.Unmarshal(body, &rawMap); err == nil {
		if _, hasCode := rawMap["code"]; hasCode {
			var envelope toobitResponse[T]
			if err := json.Unmarshal(body, &envelope); err != nil {
				var zero T
				return zero, fmt.Errorf("unmarshal envelope: %w", err)
			}
			codeStr := getResponseCodeStr(envelope.Code)

			if codeStr != "200" && codeStr != "0" && codeStr != successCode && codeStr != "" {
				var zero T
				return zero, fmt.Errorf("API error code %s: %s", codeStr, envelope.Message)
			}
			return envelope.Data, nil
		}
	}

	var data T
	if err := json.Unmarshal(body, &data); err != nil {
		var zero T
		return zero, fmt.Errorf("unmarshal raw object: %w", err)
	}
	return data, nil
}

type toobitCreateOrderResponse struct {
	OrderID       string `json:"orderId"`
	ClientOrderId string `json:"clientOrderId"`
}

type toobitOrder struct {
	OrderID       string      `json:"orderId"`
	Symbol        string      `json:"symbol"`
	Price         string      `json:"price"`
	Qty           string      `json:"qty"`
	Quantity      string      `json:"quantity"` // fallback
	OrigQty       string      `json:"origQty"`  // V1
	AvgPrice      string      `json:"avgPrice"`
	CumQty        string      `json:"cumQty"`
	ExecutedQty   string      `json:"executedQty"` // fallback & V1
	Status        string      `json:"status"`
	ClientOrderId string      `json:"clientOrderId"`
	Side          string      `json:"side"`
	PositionSide  string      `json:"positionSide"` // V2
	Type          string      `json:"type"`
	Time          json.Number `json:"time"`
	UpdateTime    json.Number `json:"updateTime"`
}

type toobitPosition struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	AvgPrice      string `json:"avgPrice"`
	Position      string `json:"position"`
	UnrealizedPnl string `json:"unrealizedPnl"`
	Leverage      string `json:"leverage"`
}

func mapSideAndPositionSide(side domain.Side, isHedge bool) (string, string) {
	ordSide := sideBuy
	posSide := posSideLong

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
			// satisfy exhaustive
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			ordSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			ordSide = sideSell
		default:
			// satisfy exhaustive
		}
		posSide = posSideBoth
	}
	return ordSide, posSide
}

// CreateOrder submits a new order to the exchange.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	params := map[string]string{
		symbolKey:  req.Symbol,
		"quantity": strconv.FormatFloat(req.Vol, 'f', -1, 64),
	}

	isHedge := req.PositionMode != domain.PositionModeOneWay
	ordSide, posSide := mapSideAndPositionSide(req.Side, isHedge)

	params["side"] = ordSide
	params["positionSide"] = posSide

	// Map Order Type and TimeInForce
	var orderType string
	var tif string
	switch req.Type {
	case exchange.OrderTypeMarket:
		orderType = orderTypeMarket
	case exchange.OrderTypePostOnly:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifPOSTONLY
	case exchange.OrderTypeIOC:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifIOC
	case exchange.OrderTypeFOK:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifFOK
	case exchange.OrderTypeLimit:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifGTC
	default:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifGTC
	}
	params[typeKey] = orderType
	if tif != "" {
		params["timeInForce"] = tif
	}

	if req.ExternalOID != "" {
		params["newClientOrderId"] = req.ExternalOID
	}

	if req.StopLossPrice > 0 {
		params["stopLoss"] = strconv.FormatFloat(req.StopLossPrice, 'f', -1, 64)
	}
	if req.TakeProfitPrice > 0 {
		params["takeProfit"] = strconv.FormatFloat(req.TakeProfitPrice, 'f', -1, 64)
	}

	body, err := c.request(ctx, http.MethodPost, "/api/v2/futures/order", params, true)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	data, err := parseResponse[toobitCreateOrderResponse](body)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.StopLossPrice > 0 || req.TakeProfitPrice > 0

	return exchange.CreateOrderResult{
		OrderID:       data.OrderID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

// CancelOrder cancels an active order.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	params := map[string]string{
		symbolKey:  symbol,
		orderIDKey: orderID,
	}
	body, err := c.request(ctx, http.MethodDelete, "/api/v2/futures/order", params, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	params := map[string]string{
		"ids": strings.Join(orderIDs, ","),
	}
	body, err := c.request(ctx, http.MethodDelete, "/api/v1/futures/cancelOrderByIds", params, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	params := map[string]string{
		symbolKey: symbol,
	}
	body, err := c.request(ctx, http.MethodDelete, "/api/v1/futures/batchOrders", params, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

// GetOrder queries an order by ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		orderIDKey: orderID,
		typeKey:    orderTypeLimit,
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/order", params, true)
	if err != nil {
		return nil, err
	}
	data, err := parseResponse[toobitOrder](body)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfo(&data), nil
}

// GetOrderByExternalID queries an order by clientOrderId.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		"origClientOrderId": externalOrderID,
		typeKey:             orderTypeLimit,
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/order", params, true)
	if err != nil {
		return nil, err
	}
	data, err := parseResponse[toobitOrder](body)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfo(&data), nil
}

// GetOpenOrders queries all active orders for a symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{
		typeKey: orderTypeLimit,
	}
	if symbol != "" {
		params[symbolKey] = symbol
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/openOrders", params, true)
	if err != nil {
		return nil, err
	}
	data, err := parseResponse[[]toobitOrder](body)
	if err != nil {
		return nil, err
	}

	infos := make([]exchange.OrderInfo, 0, len(data))
	for i := range data {
		infos = append(infos, *c.toOrderInfo(&data[i]))
	}
	return infos, nil
}

type toobitPositionsRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

func (c *Client) getRawOpenPositions(ctx context.Context, req toobitPositionsRequest) ([]toobitPosition, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[symbolKey] = req.Symbol
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/positions", params, true)
	if err != nil {
		return nil, fmt.Errorf("toobit get raw open positions: %w", err)
	}
	data, err := parseResponse[[]toobitPosition](body)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// GetOpenPositions returns all open futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	data, err := c.getRawOpenPositions(ctx, toobitPositionsRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	var positions []exchange.Position
	for i := range data {
		raw := &data[i]
		if symbol != "" && raw.Symbol != symbol {
			continue
		}
		vol := decmath.ParseFloat(raw.Position)
		if vol <= 0 {
			continue
		}

		pType := exchange.PositionTypeLong
		if raw.Side == posSideShort {
			pType = exchange.PositionTypeShort
		}

		avgPrice := decmath.ParseFloat(raw.AvgPrice)
		pnl := decmath.ParseFloat(raw.UnrealizedPnl)

		positions = append(positions, exchange.Position{
			Symbol:          raw.Symbol,
			HoldVol:         vol,
			PositionType:    pType,
			OpenAvgPrice:    avgPrice,
			HoldAvgPrice:    avgPrice,
			CloseProfitLoss: pnl,
		})
	}
	return positions, nil
}

// ClosePosition closes a position by submitting a market reduction order.
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
		if pos.PositionType == exchange.PositionTypeShort {
			closeSide = domain.SideCloseShort
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1)
	}

	return nil
}

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	params := map[string]string{
		symbolKey:  req.Symbol,
		"leverage": strconv.Itoa(req.Leverage),
	}
	body, err := c.request(ctx, http.MethodPost, "/api/v2/futures/leverage", params, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED).
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	mgnType := "CROSS"
	if marginMode == marginIsolated {
		mgnType = marginIsolated
	}
	params := map[string]string{
		symbolKey:    symbol,
		"marginType": mgnType,
	}
	body, err := c.request(ctx, http.MethodPost, "/api/v1/futures/marginType", params, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

func mapToobitSide(toobitSide, positionSide string) domain.Side {
	switch toobitSide {
	case "BUY_OPEN":
		return exchange.SideOpenLong
	case "SELL_OPEN":
		return exchange.SideOpenShort
	case "BUY_CLOSE":
		return exchange.SideCloseShort
	case "SELL_CLOSE":
		return exchange.SideCloseLong
	case "BUY":
		if positionSide == posSideLong {
			return exchange.SideOpenLong
		}
		return exchange.SideCloseShort
	case "SELL":
		if positionSide == posSideLong {
			return exchange.SideCloseLong
		}
		return exchange.SideOpenShort
	default:
		return exchange.SideOpenLong
	}
}

func (c *Client) toOrderInfo(o *toobitOrder) *exchange.OrderInfo {
	var state domain.OrderState
	switch o.Status {
	case "NEW", "ORDER_NEW":
		state = domain.OrderStateNew
	case "PARTIALLY_FILLED":
		state = domain.OrderStatePartiallyFilled
	case "FILLED", "ORDER_FILLED":
		state = domain.OrderStateFilled
	case "CANCELED", "ORDER_CANCELED", "REJECTED", "EXPIRED":
		state = domain.OrderStateCanceled
	default:
		state = domain.OrderStateNew
	}

	side := mapToobitSide(o.Side, o.PositionSide)

	vol := decmath.ParseFloat(o.Qty)
	if o.OrigQty != "" {
		vol = decmath.ParseFloat(o.OrigQty)
	} else if o.Quantity != "" {
		vol = decmath.ParseFloat(o.Quantity)
	}

	cumQty := decmath.ParseFloat(o.CumQty)
	if o.ExecutedQty != "" {
		cumQty = decmath.ParseFloat(o.ExecutedQty)
	}

	price := decmath.ParseFloat(o.Price)
	avgPrice := decmath.ParseFloat(o.AvgPrice)

	posMode := domain.PositionModeHedge
	if o.PositionSide == posSideBoth {
		posMode = domain.PositionModeOneWay
	}

	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          vol,
		DealAvgPrice: avgPrice,
		DealVol:      cumQty,
		State:        state,
		ExternalOID:  o.ClientOrderId,
		Side:         side,
		PositionMode: posMode,
		CreateTime:   parseTime(o.Time),
		UpdateTime:   parseTime(o.UpdateTime),
	}
}

func parseTime(n json.Number) int64 {
	val, err := n.Int64()
	if err != nil {
		return 0
	}
	return val
}

type toobitHistoryPosition struct {
	Symbol                string      `json:"symbol"`
	Side                  string      `json:"side"`
	Position              string      `json:"position"`
	OpenValue             string      `json:"openValue"`
	CloseValue            string      `json:"closeValue"`
	CloseTotalQty         string      `json:"closeTotalQty"`
	RealizedPnL           string      `json:"realizedPnL"`
	RealizedPnlRate       string      `json:"realizedPnlRate"`
	RealizedPnlWithoutFee string      `json:"realizedPnlWithoutFee"`
	Status                string      `json:"status"`
	OpenAvgPrice          string      `json:"openAvgPrice"`
	CloseAvgPrice         string      `json:"closeAvgPrice"`
	OpenFee               string      `json:"openFee"`
	CloseFee              string      `json:"closeFee"`
	OpenTime              json.Number `json:"openTime"`
	CloseTime             json.Number `json:"closeTime"`
	ID                    string      `json:"id"`
}

type toobitFuturesBalanceFlowRow struct {
	ID            json.Number `json:"id"`
	Coin          string      `json:"coin"`
	FlowTypeValue int         `json:"flowTypeValue"`
	FlowType      string      `json:"flowType"`
	FlowName      string      `json:"flowName"`
	Change        string      `json:"change"`
	Total         string      `json:"total"`
	Created       json.Number `json:"created"`
}
