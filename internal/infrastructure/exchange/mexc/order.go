package mexc

import (
	"context"
	"encoding/json"
	"fmt"

	"crypto-bot/internal/infrastructure/exchange"
)

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/create", req)
	if err != nil {
		return "", err
	}

	var resp APIResponse[exchange.CreateOrderResponse]
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse create order response: %w", err)
	}
	if !resp.Success {
		return "", fmt.Errorf("create order failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data.OrderID, nil
}

// CreateTrackOrder submits a new native trailing stop order and returns the order ID.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	body, err := c.PostCtx(ctx, "/api/v1/private/trackorder/place", req)
	if err != nil {
		return "", err
	}

	// For trackorder/place, the data field is a string containing the order ID
	var resp APIResponse[string]
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("parse create track order response: %w", err)
	}
	if !resp.Success {
		return "", fmt.Errorf("create track order failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// CancelOrders cancels one or more orders by their IDs.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel", orderIDs)
	if err != nil {
		return err
	}

	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse cancel order response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("cancel order failed [%d]: %s", resp.Code, resp.Message)
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	req := map[string]string{"symbol": symbol}
	body, err := c.PostCtx(ctx, "/api/v1/private/order/cancel_all", req)
	if err != nil {
		return err
	}

	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse cancel all open orders response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("cancel all open orders failed [%d]: %s", resp.Code, resp.Message)
	}
	return nil
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

	var resp APIResponse[exchange.OrderInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse get order response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get order failed [%d]: %s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

// GetOpenOrders returns all open orders, optionally filtered by symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{}
	if symbol != "" {
		params["symbol"] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/order/open_orders/", params)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]exchange.OrderInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse open orders response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get open orders failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	req := map[string]string{"symbol": symbol}
	body, err := c.PostCtx(ctx, "/api/v1/private/position/close_all", req)
	if err != nil {
		return err
	}

	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse close all response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("close all positions failed [%d]: %s", resp.Code, resp.Message)
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/position/change_leverage", req)
	if err != nil {
		return err
	}

	var resp APIResponse[json.RawMessage]
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("parse change leverage response: %w", err)
	}
	if !resp.Success {
		return fmt.Errorf("change leverage failed [%d]: %s", resp.Code, resp.Message)
	}
	return nil
}
