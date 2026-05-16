package mexc

import (
	"context"
	"fmt"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/create", req)
	if err != nil {
		return "", err
	}

	data, err := ParseResponse[exchange.CreateOrderResponse](body, "create_order")
	if err != nil {
		return "", err
	}
	return data.OrderID, nil
}

// CreateTrackOrder submits a new native trailing stop order and returns the order ID.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/trackorder/place", req)
	if err != nil {
		return "", err
	}

	// For trackorder/place, the data field is a string containing the order ID.
	data, err := ParseResponse[string](body, "create_track_order")
	if err != nil {
		return "", err
	}
	return data, nil
}

// CancelOrders cancels one or more orders by their IDs.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel", orderIDs)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "cancel_orders")
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	req := map[string]string{paramSymbol: symbol}
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel_all", req)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "cancel_all_open_orders")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return c.CancelOrders(ctx, []string{orderID})
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	path := fmt.Sprintf("/api/v1/private/order/get/%s", orderID)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	data, err := ParseResponse[exchange.OrderInfo](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &data, nil
}

// GetOpenOrders returns all open orders, optionally filtered by symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/order/open_orders/", params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]exchange.OrderInfo](body, "get_open_orders")
}

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	req := map[string]string{paramSymbol: symbol}
	body, err := c.PostCtx(ctx, "/api/v1/private/position/close_all", req)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "close_all_positions")
}

// ClosePosition closes one position leg using a reduce-only market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         int(closeSide),
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
	}
	body, err := c.PostCtx(ctx, "/api/v1/private/order/create", req)
	if err != nil {
		return err
	}
	_, err = ParseResponse[exchange.CreateOrderResponse](body, "close_position")
	return err
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/position/change_leverage", req)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "change_leverage")
}
