package hyperliquid

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/samber/lo"
	hl "github.com/sonirico/go-hyperliquid"
)

// CreateOrder places a new order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	if c.exchange == nil {
		return "", fmt.Errorf("trading is disabled: exchange signer is not configured")
	}

	isBuy := req.Side != exchange.SideOpenShort && req.Side != exchange.SideCloseLong

	var tif string
	switch req.Type {
	case exchange.OrderTypeIOC:
		tif = tifIoc
	case exchange.OrderTypePostOnly:
		tif = tifAlo
	default:
		tif = tifGtc
	}

	orderReq := hl.CreateOrderRequest{
		Coin:       req.Symbol,
		IsBuy:      isBuy,
		Price:      req.Price,
		Size:       req.Vol,
		ReduceOnly: req.ReduceOnly,
		OrderType: hl.OrderType{
			Limit: &hl.LimitOrderType{
				Tif: hl.Tif(tif),
			},
		},
	}

	if req.ExternalOID != "" {
		orderReq.ClientOrderID = &req.ExternalOID
	}

	status, err := c.exchange.Order(ctx, orderReq, nil)
	if err != nil {
		return "", err
	}

	if status.Error != nil {
		return "", fmt.Errorf("hyperliquid order placement failed: %s", lo.FromPtr(status.Error))
	}

	var oid int64
	if status.Resting != nil {
		oid = status.Resting.Oid
	} else if status.Filled != nil {
		oid = int64(status.Filled.Oid)
	}

	return strconv.FormatInt(oid, 10), nil
}

// CancelOrder cancels a single order.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if c.exchange == nil {
		return fmt.Errorf("trading is disabled: exchange signer is not configured")
	}

	oid, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid orderID format: %w", err)
	}

	resp, err := c.exchange.Cancel(ctx, symbol, oid)
	if err != nil {
		return err
	}

	if resp.Status != "ok" {
		return fmt.Errorf("failed to cancel order on Hyperliquid")
	}

	return nil
}

// CancelOrders is a stub.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch cancel not supported on Hyperliquid without symbols")
}

// GetOrder queries a single order.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	oid, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid orderID format: %w", err)
	}

	res, err := c.info.QueryOrderByOid(ctx, c.userAddress, oid)
	if err != nil {
		return nil, err
	}

	if res.Order.Order.Oid == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	o := res.Order.Order
	price, _ := strconv.ParseFloat(o.LimitPx, 64)
	origSz, _ := strconv.ParseFloat(o.OrigSz, 64)

	state := exchange.OrderStatePartial
	switch res.Order.Status {
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	default:
	}

	return &exchange.OrderInfo{
		OrderID:    orderID,
		Symbol:     o.Coin,
		Price:      price,
		Vol:        origSz,
		State:      state,
		CreateTime: o.Timestamp,
		UpdateTime: res.Order.StatusTimestamp,
	}, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	openOrders, err := c.info.OpenOrders(ctx, c.userAddress)
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(openOrders))
	for i := range openOrders {
		o := &openOrders[i]
		if symbol != "" && o.Coin != symbol {
			continue
		}

		orders = append(orders, exchange.OrderInfo{
			OrderID:    strconv.FormatInt(o.Oid, 10),
			Symbol:     o.Coin,
			Price:      o.LimitPx,
			Vol:        o.OrigSz,
			State:      exchange.OrderStatePartial,
			CreateTime: o.Timestamp,
			UpdateTime: o.Timestamp,
		})
	}
	return orders, nil
}

// ClosePosition closes a position with opposite order side.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	if c.exchange == nil {
		return fmt.Errorf("trading is disabled: exchange signer is not configured")
	}

	isBuy := closeSide != domain.SideCloseLong

	orderReq := hl.CreateOrderRequest{
		Coin:       symbol,
		IsBuy:      isBuy,
		Price:      0.0,
		Size:       volume,
		ReduceOnly: true,
		OrderType: hl.OrderType{
			Limit: &hl.LimitOrderType{
				Tif: hl.Tif(tifIoc),
			},
		},
	}

	status, err := c.exchange.Order(ctx, orderReq, nil)
	if err != nil {
		return err
	}

	if status.Error != nil {
		return fmt.Errorf("close position order failed: %s", lo.FromPtr(status.Error))
	}

	return nil
}

// ChangeLeverage updates leverage on Hyperliquid.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	if c.exchange == nil {
		return fmt.Errorf("trading is disabled: exchange signer is not configured")
	}

	isCross := req.OpenType == exchange.OpenTypeCross
	_, err := c.exchange.UpdateLeverage(ctx, req.Leverage, req.Symbol, isCross)
	return err
}

// CreateTrackOrder is a stub.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("track orders not supported on Hyperliquid")
}

// CancelAllOpenOrders is a stub.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	orders, err := c.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}
	for i := range orders {
		_ = c.CancelOrder(ctx, orders[i].Symbol, orders[i].OrderID)
	}
	return nil
}

// CloseAllPositions is a stub.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	return nil
}
