package futures

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"

	"github.com/google/uuid"
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
		if err := xjson.Unmarshal(body, &data); err != nil {
			var zero T
			return zero, fmt.Errorf("unmarshal raw array: %w", err)
		}
		return data, nil
	}

	var rawMap map[string]any
	if err := xjson.Unmarshal(body, &rawMap); err == nil {
		if _, hasCode := rawMap["code"]; hasCode {
			var envelope toobitResponse[T]
			if err := xjson.Unmarshal(body, &envelope); err != nil {
				var zero T
				return zero, fmt.Errorf("unmarshal envelope: %w", err)
			}
			codeStr := getResponseCodeStr(envelope.Code)

			if codeStr != "200" && codeStr != "0" && codeStr != "200000" && codeStr != "" {
				var zero T
				return zero, fmt.Errorf("API error code %s: %s", codeStr, envelope.Message)
			}
			return envelope.Data, nil
		}
	}

	var data T
	if err := xjson.Unmarshal(body, &data); err != nil {
		var zero T
		return zero, fmt.Errorf("unmarshal raw object: %w", err)
	}
	return data, nil
}

type toobitOrder struct {
	OrderID       string       `json:"orderId"`
	Symbol        string       `json:"symbol"`
	Price         string       `json:"price"`
	Qty           string       `json:"qty"`
	Quantity      string       `json:"quantity"`
	OrigQty       string       `json:"origQty"`
	AvgPrice      string       `json:"avgPrice"`
	CumQty        string       `json:"cumQty"`
	ExecutedQty   string       `json:"executedQty"`
	Status        string       `json:"status"`
	ClientOrderId string       `json:"clientOrderId"`
	Side          string       `json:"side"`
	PositionSide  string       `json:"positionSide"`
	Type          string       `json:"type"`
	Time          xjson.Number `json:"time"`
	UpdateTime    xjson.Number `json:"updateTime"`
}

func (c *Client) rawCreateOrder(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodPost, "/api/v2/futures/order", params, true)
}

func (c *Client) rawCancelOrder(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodDelete, "/api/v2/futures/order", params, true)
}

func (c *Client) rawCancelOrders(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodDelete, "/api/v1/futures/cancelOrderByIds", params, true)
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodDelete, "/api/v1/futures/batchOrders", params, true)
}

func (c *Client) rawGetOrder(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodGet, "/api/v1/futures/order", params, true)
}

func (c *Client) rawGetOpenOrders(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.base.Request(ctx, http.MethodGet, "/api/v1/futures/openOrders", params, true)
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

	var orderType string
	var tif string
	switch req.Type {
	case exchange.OrderTypeMarket:
		orderType = orderTypeMarket
	case exchange.OrderTypePostOnly:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = "POST_ONLY"
	case exchange.OrderTypeIOC:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = "IOC"
	case exchange.OrderTypeFOK:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = "FOK"
	case exchange.OrderTypeLimit:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifGTC
	default:
		orderType = orderTypeLimit
		params["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		tif = tifGTC
	}
	params["type"] = orderType
	if tif != "" {
		params["timeInForce"] = tif
	}

	clientOid := req.ExternalOID
	if clientOid == "" {
		clientOid = uuid.NewString()
	}
	params["newClientOrderId"] = clientOid

	if req.StopLossPrice > 0 {
		params["stopLoss"] = strconv.FormatFloat(req.StopLossPrice, 'f', -1, 64)
	}
	if req.TakeProfitPrice > 0 {
		params["takeProfit"] = strconv.FormatFloat(req.TakeProfitPrice, 'f', -1, 64)
	}

	body, err := c.rawCreateOrder(ctx, params)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	data, err := parseResponse[toobitOrder](body)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.StopLossPrice > 0 || req.TakeProfitPrice > 0

	return exchange.CreateOrderResult{
		OrderID:       data.OrderID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	params := map[string]string{
		symbolKey:  symbol,
		orderIDKey: orderID,
	}
	body, err := c.rawCancelOrder(ctx, params)
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
	body, err := c.rawCancelOrders(ctx, params)
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
	body, err := c.rawCancelAllOpenOrders(ctx, params)
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
	body, err := c.rawGetOrder(ctx, params)
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
		"type":              "LIMIT",
	}
	body, err := c.rawGetOrder(ctx, params)
	if err != nil {
		return nil, err
	}
	data, err := parseResponse[toobitOrder](body)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfo(&data), nil
}

// GetOpenOrders retrieves all open orders for a symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[symbolKey] = symbol
	}
	body, err := c.rawGetOpenOrders(ctx, params)
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
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			ordSide = sideBuy
		case exchange.SideOpenShort, exchange.SideCloseLong:
			ordSide = sideSell
		default:
		}
		posSide = posSideBoth
	}
	return ordSide, posSide
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
	case sideBuy:
		if positionSide == posSideLong {
			return exchange.SideOpenLong
		}
		return exchange.SideCloseShort
	case sideSell:
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
	if o.PositionSide == "BOTH" {
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

func parseTime(n xjson.Number) int64 {
	val, err := n.Int64()
	if err != nil {
		return 0
	}
	return val
}
