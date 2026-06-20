package deepcoin

import (
	"context"
	"fmt"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	return exchange.CreateOrderResult{}, fmt.Errorf("CreateOrder not supported on Deepcoin")
}

func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not supported on Deepcoin")
}

func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return fmt.Errorf("CancelOrder not supported on Deepcoin")
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("CancelOrders not supported on Deepcoin")
}

func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return fmt.Errorf("CancelAllOpenOrders not supported on Deepcoin")
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	return nil, fmt.Errorf("GetOrder not supported on Deepcoin")
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	return nil, fmt.Errorf("GetOrderByExternalID not supported on Deepcoin")
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	return nil, fmt.Errorf("GetOpenOrders not supported on Deepcoin")
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	return fmt.Errorf("ClosePosition not supported on Deepcoin")
}

func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	return fmt.Errorf("CloseAllPositions not supported on Deepcoin")
}

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return fmt.Errorf("ChangeLeverage not supported on Deepcoin")
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return fmt.Errorf("SwitchMarginMode not supported on Deepcoin")
}
