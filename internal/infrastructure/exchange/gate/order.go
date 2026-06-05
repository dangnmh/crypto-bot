package gate

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/antihax/optional"
	"github.com/gateio/gateapi-go/v7"
)

// Explicit request/response structs for order endpoints.

type gateCreateOrderRequest struct {
	Settle string               `json:"settle"`
	Order  gateapi.FuturesOrder `json:"order"`
}

type gateCancelOrderRequest struct {
	Settle  string `json:"settle"`
	OrderID string `json:"orderId"`
}

type gateCancelAllOpenOrdersRequest struct {
	Settle string `json:"settle"`
	Symbol string `json:"symbol"`
}

type gateGetOrderRequest struct {
	Settle  string `json:"settle"`
	OrderID string `json:"orderId"`
}

type gateListOpenOrdersRequest struct {
	Settle string `json:"settle"`
	Symbol string `json:"symbol,omitempty"`
}

type gateChangeLeverageRequest struct {
	Settle   string `json:"settle"`
	Symbol   string `json:"symbol"`
	Leverage string `json:"leverage"`
}

// Private raw methods invoking the Gate.io SDK.

func (c *Client) createRawOrder(ctx context.Context, req gateCreateOrderRequest) (*gateapi.FuturesOrder, error) {
	ctx = c.authCtx(ctx)
	resp, httpResp, err := c.apiClient.FuturesApi.CreateFuturesOrder(ctx, req.Settle, req.Order, nil)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req gateCancelOrderRequest) error {
	ctx = c.authCtx(ctx)
	_, httpResp, err := c.apiClient.FuturesApi.CancelFuturesOrder(ctx, req.Settle, req.OrderID, nil)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	return err
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, req gateCancelAllOpenOrdersRequest) error {
	ctx = c.authCtx(ctx)
	_, httpResp, err := c.apiClient.FuturesApi.CancelFuturesOrders(ctx, req.Settle, req.Symbol, nil)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	return err
}

func (c *Client) getRawOrder(ctx context.Context, req gateGetOrderRequest) (*gateapi.FuturesOrder, error) {
	ctx = c.authCtx(ctx)
	resp, httpResp, err := c.apiClient.FuturesApi.GetFuturesOrder(ctx, req.Settle, req.OrderID)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req gateListOpenOrdersRequest) ([]gateapi.FuturesOrder, error) {
	ctx = c.authCtx(ctx)
	opts := &gateapi.ListFuturesOrdersOpts{
		Contract: optional.NewString(req.Symbol),
	}
	resp, httpResp, err := c.apiClient.FuturesApi.ListFuturesOrders(ctx, req.Settle, "open", opts)
	if httpResp != nil && httpResp.Body != nil {
		defer func() { _ = httpResp.Body.Close() }()
	}
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, req gateChangeLeverageRequest) error {
	ctx = c.authCtx(ctx)
	_, httpResp, err := c.apiClient.FuturesApi.UpdatePositionLeverage(ctx, req.Settle, req.Symbol, req.Leverage, nil)
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	if err != nil {
		_, httpRespDual, errDual := c.apiClient.FuturesApi.UpdateDualModePositionLeverage(ctx, req.Settle, req.Symbol, req.Leverage, nil)
		if httpRespDual != nil && httpRespDual.Body != nil {
			_ = httpRespDual.Body.Close()
		}
		if errDual != nil {
			return fmt.Errorf("gate.io update leverage error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	order := c.mapSubmitOrder(req)
	resp, err := c.createRawOrder(ctx, gateCreateOrderRequest{
		Settle: gateSettleUsdt,
		Order:  order,
	})
	if err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("gate.io create order: %w", err)
	}

	return exchange.CreateOrderResult{OrderID: strconv.FormatInt(resp.Id, 10), TPSLSubmitted: false}, nil
}

// CreateTrackOrder submits a trailing stop order. Stubbed since track orders are not used in Core reversion.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Gate.io")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	err := c.cancelRawOrder(ctx, gateCancelOrderRequest{
		Settle:  gateSettleUsdt,
		OrderID: orderID,
	})
	if err != nil {
		return fmt.Errorf("gate.io cancel order: %w", err)
	}
	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		err := c.cancelRawOrder(ctx, gateCancelOrderRequest{
			Settle:  gateSettleUsdt,
			OrderID: id,
		})
		if err != nil {
			return fmt.Errorf("gate.io cancel bulk order %s: %w", id, err)
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	err := c.cancelRawAllOpenOrders(ctx, gateCancelAllOpenOrdersRequest{
		Settle: gateSettleUsdt,
		Symbol: symbol,
	})
	if err != nil {
		return fmt.Errorf("gate.io cancel all open orders for %s: %w", symbol, err)
	}
	return nil
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	resp, err := c.getRawOrder(ctx, gateGetOrderRequest{
		Settle:  gateSettleUsdt,
		OrderID: orderID,
	})
	if err != nil {
		return nil, fmt.Errorf("gate.io get order %s: %w", orderID, err)
	}

	info := mapOrderInfo(*resp)
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	resp, err := c.getRawOpenOrders(ctx, gateListOpenOrdersRequest{
		Settle: gateSettleUsdt,
		Symbol: symbol,
	})
	if err != nil {
		return nil, fmt.Errorf("gate.io get open orders for %s: %w", symbol, err)
	}

	orders := make([]exchange.OrderInfo, 0, len(resp))
	for i := range resp {
		raw := &resp[i]
		orders = append(orders, mapOrderInfo(*raw))
	}
	return orders, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         int(closeSide),
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
			if pos.PositionType == 1 { // Long
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
	return c.changeRawLeverage(ctx, gateChangeLeverageRequest{
		Settle:   gateSettleUsdt,
		Symbol:   req.Symbol,
		Leverage: leverageStr,
	})
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Gate.io.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	ctx = c.authCtx(ctx)
	settle := gateSettleUsdt
	modeStr := "isolated"
	if marginMode == "CROSS" {
		modeStr = "cross"
	}

	opts := gateapi.FuturesPositionCrossMode{
		Contract: symbol,
		Mode:     modeStr,
	}
	_, httpResp, err := c.apiClient.FuturesApi.UpdatePositionCrossMode(ctx, settle, opts)
	if httpResp != nil && httpResp.Body != nil {
		_ = httpResp.Body.Close()
	}
	if err != nil {
		inlineOpts := gateapi.InlineObject{
			Contract: symbol,
			Mode:     modeStr,
		}
		_, httpRespDual, errDual := c.apiClient.FuturesApi.UpdateDualCompPositionCrossMode(ctx, settle, inlineOpts)
		if httpRespDual != nil && httpRespDual.Body != nil {
			_ = httpRespDual.Body.Close()
		}
		if errDual != nil {
			return fmt.Errorf("gate.io update margin mode error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Helper mapping functions.

// mapSubmitOrder maps a SubmitOrderRequest to gateapi.FuturesOrder.
func (c *Client) mapSubmitOrder(req exchange.SubmitOrderRequest) gateapi.FuturesOrder {
	order := gateapi.FuturesOrder{
		Contract: req.Symbol,
	}

	if req.Type == exchange.OrderTypeMarket {
		order.Price = "0"
		order.Tif = gateTifIOC
	} else {
		order.Price = fmt.Sprintf("%g", req.Price)
		switch req.Type {
		case exchange.OrderTypePostOnly:
			order.Tif = "poc"
		case exchange.OrderTypeIOC:
			order.Tif = gateTifIOC
		case exchange.OrderTypeFOK:
			order.Tif = "fok"
		default:
			order.Tif = "gtc"
		}
	}

	isHedge := req.PositionMode == 1
	volInt := int64(req.Vol)

	if isHedge {
		switch req.Side {
		case exchange.SideOpenLong:
			order.Size = volInt
		case exchange.SideOpenShort:
			order.Size = -volInt
		case exchange.SideCloseLong:
			order.Size = 0
			order.AutoSize = "close_long"
		case exchange.SideCloseShort:
			order.Size = 0
			order.AutoSize = "close_short"
		}
	} else {
		switch req.Side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			order.Size = volInt
		case exchange.SideOpenShort, exchange.SideCloseLong:
			order.Size = -volInt
		}
		if req.ReduceOnly {
			order.Close = true
		}
	}

	if req.ExternalOID != "" {
		order.Text = "t-" + req.ExternalOID
	}

	return order
}

// mapOrderInfo maps a gateapi.FuturesOrder to exchange.OrderInfo.
func mapOrderInfo(raw gateapi.FuturesOrder) exchange.OrderInfo {
	info := exchange.OrderInfo{
		OrderID:      strconv.FormatInt(raw.Id, 10),
		Symbol:       raw.Contract,
		Price:        decmath.ParseFloat(raw.Price),
		Vol:          float64(decmath.AbsInt64(raw.Size)),
		DealAvgPrice: decmath.ParseFloat(raw.FillPrice),
		DealVol:      float64(decmath.AbsInt64(raw.Size) - decmath.AbsInt64(raw.Left)),
		CreateTime:   int64(raw.CreateTime * 1000),
		UpdateTime:   int64(raw.FinishTime * 1000),
	}

	if raw.Size > 0 {
		info.Side = exchange.SideOpenLong
	} else if raw.Size < 0 {
		info.Side = exchange.SideOpenShort
	}

	switch raw.Status {
	case gateOrderStatusFinished:
		switch raw.FinishAs {
		case gateFinishAsFilled:
			info.State = exchange.OrderStateFilled
		case "cancelled", gateTifIOC:
			info.State = exchange.OrderStateCanceled
		}
	case gateOrderStatusOpen:
		if raw.Left < raw.Size {
			info.State = exchange.OrderStatePartial
		}
	}

	if after, ok := strings.CutPrefix(raw.Text, "t-"); ok {
		info.ExternalOID = after
	} else {
		info.ExternalOID = raw.Text
	}

	return info
}
