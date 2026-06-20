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

type hyperliquidCreateOrderRequest struct {
	Coin          string
	IsBuy         bool
	Price         float64
	Size          float64
	ReduceOnly    bool
	Tif           string
	ClientOrderID *string
}

type hyperliquidCancelOrderRequest struct {
	Symbol  string
	OrderID int64
}

type hyperliquidQueryOrderRequest struct {
	UserAddress string
	OrderID     int64
}

type hyperliquidOpenOrdersRequest struct {
	UserAddress string
}

type hyperliquidUpdateLeverageRequest struct {
	Leverage int
	Symbol   string
	IsCross  bool
}

// Private raw methods invoking the Hyperliquid API or SDK.

func (c *Client) createRawOrder(ctx context.Context, req hyperliquidCreateOrderRequest) (hl.OrderStatus, error) {
	orderReq := hl.CreateOrderRequest{
		Coin:       req.Coin,
		IsBuy:      req.IsBuy,
		Price:      req.Price,
		Size:       req.Size,
		ReduceOnly: req.ReduceOnly,
		OrderType: hl.OrderType{
			Limit: &hl.LimitOrderType{
				Tif: hl.Tif(req.Tif),
			},
		},
	}
	if req.ClientOrderID != nil {
		orderReq.ClientOrderID = req.ClientOrderID
	}
	return c.exchange.Order(ctx, orderReq, nil)
}

func (c *Client) cancelRawOrder(ctx context.Context, req hyperliquidCancelOrderRequest) (*hl.APIResponse[hl.CancelOrderResponse], error) {
	return c.exchange.Cancel(ctx, req.Symbol, req.OrderID)
}

func (c *Client) getRawOrder(ctx context.Context, req hyperliquidQueryOrderRequest) (*hl.OrderQueryResult, error) {
	return c.info.QueryOrderByOid(ctx, req.UserAddress, req.OrderID)
}

func (c *Client) getRawOpenOrders(ctx context.Context, req hyperliquidOpenOrdersRequest) ([]hl.OpenOrder, error) {
	return c.info.OpenOrders(ctx, req.UserAddress)
}

func (c *Client) changeRawLeverage(ctx context.Context, req hyperliquidUpdateLeverageRequest) (*hl.UserState, error) {
	return c.exchange.UpdateLeverage(ctx, req.Leverage, req.Symbol, req.IsCross)
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder places a new order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	if c.exchange == nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("trading is disabled: exchange signer is not configured")
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

	rawReq := hyperliquidCreateOrderRequest{
		Coin:       req.Symbol,
		IsBuy:      isBuy,
		Price:      req.Price,
		Size:       req.Vol,
		ReduceOnly: req.ReduceOnly,
		Tif:        tif,
	}

	if req.ExternalOID != "" {
		rawReq.ClientOrderID = &req.ExternalOID
	}

	status, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	if status.Error != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("hyperliquid order placement failed: %s", lo.FromPtr(status.Error))
	}

	var oid int64
	if status.Resting != nil {
		oid = status.Resting.Oid
	} else if status.Filled != nil {
		oid = int64(status.Filled.Oid)
	}

	return exchange.CreateOrderResult{
		OrderID:       strconv.FormatInt(oid, 10),
		TPSLSubmitted: false,
	}, nil
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

	resp, err := c.cancelRawOrder(ctx, hyperliquidCancelOrderRequest{
		Symbol:  symbol,
		OrderID: oid,
	})
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
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	oid, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid orderID format: %w", err)
	}

	res, err := c.getRawOrder(ctx, hyperliquidQueryOrderRequest{
		UserAddress: c.userAddress,
		OrderID:     oid,
	})
	if err != nil {
		return nil, err
	}

	if res.Order.Order.Oid == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	o := res.Order.Order
	price, _ := strconv.ParseFloat(o.LimitPx, 64)
	origSz, _ := strconv.ParseFloat(o.OrigSz, 64)

	state := exchange.OrderStateNew
	switch res.Order.Status {
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	default:
		szVal, _ := strconv.ParseFloat(o.Sz, 64)
		if szVal > 0 && szVal < origSz {
			state = exchange.OrderStatePartiallyFilled
		}
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

// GetOrderByExternalID queries order status by client order ID (cloid).
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	res, err := c.getRawOrderByCloid(ctx, hyperliquidQueryOrderByCloidRequest{
		UserAddress: c.userAddress,
		Cloid:       externalOrderID,
	})
	if err != nil {
		return nil, err
	}

	if res.Order.Order.Oid == 0 {
		return nil, fmt.Errorf("order not found by external ID: %s", externalOrderID)
	}

	o := res.Order.Order
	price, _ := strconv.ParseFloat(o.LimitPx, 64)
	origSz, _ := strconv.ParseFloat(o.OrigSz, 64)

	state := exchange.OrderStateNew
	switch res.Order.Status {
	case stateFilled:
		state = exchange.OrderStateFilled
	case stateCanceled:
		state = exchange.OrderStateCanceled
	default:
		szVal, _ := strconv.ParseFloat(o.Sz, 64)
		if szVal > 0 && szVal < origSz {
			state = exchange.OrderStatePartiallyFilled
		}
	}

	return &exchange.OrderInfo{
		OrderID:    strconv.FormatInt(o.Oid, 10),
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

	openOrders, err := c.getRawOpenOrders(ctx, hyperliquidOpenOrdersRequest{
		UserAddress: c.userAddress,
	})
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(openOrders))
	for i := range openOrders {
		o := &openOrders[i]
		if symbol != "" && o.Coin != symbol {
			continue
		}

		orderState := exchange.OrderStateNew
		if o.Size > 0 && o.Size < o.OrigSz {
			orderState = exchange.OrderStatePartiallyFilled
		}

		orders = append(orders, exchange.OrderInfo{
			OrderID:    strconv.FormatInt(o.Oid, 10),
			Symbol:     o.Coin,
			Price:      o.LimitPx,
			Vol:        o.OrigSz,
			State:      orderState,
			CreateTime: o.Timestamp,
			UpdateTime: o.Timestamp,
		})
	}
	return orders, nil
}

// ClosePosition closes a position with opposite order side.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	if c.exchange == nil {
		return fmt.Errorf("trading is disabled: exchange signer is not configured")
	}

	isBuy := closeSide != domain.SideCloseLong

	req := hyperliquidCreateOrderRequest{
		Coin:       symbol,
		IsBuy:      isBuy,
		Price:      0.0,
		Size:       volume,
		ReduceOnly: true,
		Tif:        tifIoc,
	}

	status, err := c.createRawOrder(ctx, req)
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
	_, err := c.changeRawLeverage(ctx, hyperliquidUpdateLeverageRequest{
		Leverage: req.Leverage,
		Symbol:   req.Symbol,
		IsCross:  isCross,
	})
	return err
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Hyperliquid.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	if c.exchange == nil {
		return fmt.Errorf("trading is disabled: exchange signer is not configured")
	}
	isCross := marginMode == "CROSS"
	_, err := c.changeRawLeverage(ctx, hyperliquidUpdateLeverageRequest{
		Leverage: leverage,
		Symbol:   symbol,
		IsCross:  isCross,
	})
	return err
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
