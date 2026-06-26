package mexc

import (
	"context"
	"crypto-bot/internal/infrastructure/exchange"
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for order endpoints.

type mexcCreateOrderRequest struct {
	Symbol          string  `json:"symbol"`
	Price           float64 `json:"price,omitempty"`
	Vol             float64 `json:"vol"`
	Leverage        int     `json:"leverage,omitempty"`
	Side            int     `json:"side"`
	Type            int     `json:"type"`
	OpenType        int     `json:"openType,omitempty"`
	ExternalOID     string  `json:"externalOid,omitempty"`
	PositionID      int64   `json:"positionId,omitempty"`
	PositionMode    int     `json:"positionMode,omitempty"`
	ReduceOnly      bool    `json:"reduceOnly,omitempty"`
	FlashClose      bool    `json:"flashClose,omitempty"`
	StopLossPrice   float64 `json:"stopLossPrice,omitempty"`
	TakeProfitPrice float64 `json:"takeProfitPrice,omitempty"`
}

type mexcCreateOrderResponse struct {
	OrderID string `json:"orderId"`
	Ts      int64  `json:"ts"`
}

type mexcCancelOrdersRequest []string

type mexcCancelOrderResult struct {
	OrderID   int64  `json:"orderId"`
	ErrorCode int    `json:"errorCode"`
	ErrorMsg  string `json:"errorMsg"`
}

type mexcCancelAllOpenOrdersRequest struct {
	Symbol string `json:"symbol"`
}

type mexcGetOrderRequest struct {
	OrderID string `json:"orderId"`
}

type mexcGetOrderByExternalRequest struct {
	Symbol      string `json:"symbol"`
	ExternalOID string `json:"externalOid"`
}

type mexcOpenOrdersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

// Private raw methods invoking the MEXC API.

func (c *Client) rawCreateOrder(ctx context.Context, req mexcCreateOrderRequest) (*mexcCreateOrderResponse, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/create", req)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[mexcCreateOrderResponse](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawCancelOrders(ctx context.Context, req mexcCancelOrdersRequest) ([]mexcCancelOrderResult, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel", req)
	if err != nil {
		return nil, err
	}
	return parseCancelOrdersResponse(body)
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, req mexcCancelAllOpenOrdersRequest) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel_all", req)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "cancel_all_open_orders")
}

func (c *Client) rawGetOrder(ctx context.Context, req mexcGetOrderRequest) (*mexcOrder, error) {
	body, err := c.GetOrderDetailRaw(ctx, req.OrderID, nil)
	if err != nil {
		return nil, err
	}
	data, err := ParseResponse[mexcOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) rawGetOrderByExOrderID(ctx context.Context, req mexcGetOrderByExternalRequest) (*mexcOrder, error) {
	path := fmt.Sprintf("/api/v1/private/order/external/%s/%s", req.Symbol, req.ExternalOID)
	body, err := c.RawRequest(ctx, http.MethodGet, path, nil, nil)
	if err != nil {
		return nil, err
	}
	data, err := ParseResponse[mexcOrder](body, "get_order_by_external")
	if err != nil {
		return nil, err
	}
	return &data, nil
}

func (c *Client) rawGetOpenOrders(ctx context.Context, req mexcOpenOrdersRequest) ([]mexcOrder, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v1/private/order/open_orders/", params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcOrder](body, "get_open_orders")
}

func parseCancelOrdersResponse(body []byte) ([]mexcCancelOrderResult, error) {
	var raw APIResponse[json.RawMessage]
	if err := xjson.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("parse cancel_orders response: %w", err)
	}
	if !raw.Success {
		return nil, toAPIError(raw.Code, raw.Message, "cancel_orders")
	}
	if len(raw.Data) == 0 || string(raw.Data) == "null" {
		return nil, nil
	}
	var results []mexcCancelOrderResult
	if err := xjson.Unmarshal(raw.Data, &results); err != nil {
		return nil, fmt.Errorf("parse cancel_orders data: %w", err)
	}
	return results, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	mexcReq := mexcCreateOrderRequest{
		Symbol:          req.Symbol,
		Price:           req.Price,
		Vol:             req.Vol,
		Leverage:        req.Leverage,
		Side:            int(req.Side),
		Type:            int(req.Type),
		OpenType:        int(req.OpenType),
		ExternalOID:     req.ExternalOID,
		PositionID:      req.PositionID,
		PositionMode:    int(req.PositionMode),
		ReduceOnly:      req.ReduceOnly,
		FlashClose:      req.FlashClose,
		StopLossPrice:   req.StopLossPrice,
		TakeProfitPrice: req.TakeProfitPrice,
	}

	data, err := c.rawCreateOrder(ctx, mexcReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{OrderID: data.OrderID, TPSLSubmitted: tpslSubmitted}, nil
}

// CancelOrders cancels one or more orders by their IDs.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	results, err := c.rawCancelOrders(ctx, mexcCancelOrdersRequest(orderIDs))
	if err != nil {
		return err
	}
	for _, result := range results {
		if result.ErrorCode != 0 {
			return &exchange.APIError{
				Code:    result.ErrorCode,
				Message: result.ErrorMsg,
				Path:    "cancel_orders",
			}
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return c.rawCancelAllOpenOrders(ctx, mexcCancelAllOpenOrdersRequest{Symbol: symbol})
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return c.CancelOrders(ctx, []string{orderID})
}

// GetOrder queries a single order by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.rawGetOrder(ctx, mexcGetOrderRequest{OrderID: orderID})
	if err != nil {
		return nil, err
	}
	return raw.toOrderInfo(), nil
}

// GetOrderByExternalID queries a single order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	raw, err := c.rawGetOrderByExOrderID(ctx, mexcGetOrderByExternalRequest{
		Symbol:      symbol,
		ExternalOID: externalOrderID,
	})
	if err != nil {
		return nil, err
	}
	return raw.toOrderInfo(), nil
}

// GetOpenOrders returns all open orders, optionally filtered by symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawOrders, err := c.rawGetOpenOrders(ctx, mexcOpenOrdersRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, len(rawOrders))
	for i := range rawOrders {
		orders[i] = *rawOrders[i].toOrderInfo()
	}
	return orders, nil
}
