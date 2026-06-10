package gate

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for order endpoints.

type gateChangeLeverageRequest struct {
	Symbol             string `json:"symbol"`
	Leverage           string `json:"leverage"`
	CrossLeverageLimit string `json:"cross_leverage_limit,omitempty"`
}

type gatePositionCrossModeRequest struct {
	Mode     string `json:"mode"`
	Contract string `json:"contract"`
}

// Private raw methods invoking raw HTTP requests.

func (c *Client) createRawOrder(ctx context.Context, settle string, order gateFuturesOrder) (*gateFuturesOrder, error) {
	var result gateFuturesOrder
	path := fmt.Sprintf("/futures/%s/orders", settle)
	err := c.sendRequest(ctx, "POST", path, nil, &order, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, settle, orderID string) error {
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)
	return c.sendRequest(ctx, "DELETE", path, nil, nil, nil)
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, settle, symbol string) error {
	query := url.Values{}
	query.Set("contract", symbol)
	path := fmt.Sprintf("/futures/%s/orders", settle)
	return c.sendRequest(ctx, "DELETE", path, query, nil, nil)
}

func (c *Client) getRawOrder(ctx context.Context, settle, orderID string) (*gateFuturesOrder, error) {
	var result gateFuturesOrder
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)
	err := c.sendRequest(ctx, "GET", path, nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, settle, symbol string) ([]gateFuturesOrder, error) {
	var result []gateFuturesOrder
	query := url.Values{}
	query.Set("contract", symbol)
	query.Set("status", "open")
	path := fmt.Sprintf("/futures/%s/orders", settle)
	err := c.sendRequest(ctx, "GET", path, query, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, settle string, req gateChangeLeverageRequest) error {
	query := url.Values{}
	query.Set("leverage", req.Leverage)
	if req.CrossLeverageLimit != "" {
		query.Set("cross_leverage_limit", req.CrossLeverageLimit)
	}

	path := fmt.Sprintf("/futures/%s/positions/%s/leverage", settle, req.Symbol)
	err := c.sendRequest(ctx, "POST", path, query, nil, nil)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/%s/leverage", settle, req.Symbol)
		errDual := c.sendRequest(ctx, "POST", dualPath, query, nil, nil)
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
	resp, err := c.createRawOrder(ctx, gateSettleUsdt, order)
	if err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("gate.io create order: %w", err)
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{OrderID: strconv.FormatInt(resp.Id, 10), TPSLSubmitted: tpslSubmitted}, nil
}

// CreateTrackOrder submits a trailing stop order. Stubbed since track orders are not used in Core reversion.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Gate.io")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	err := c.cancelRawOrder(ctx, gateSettleUsdt, orderID)
	if err != nil {
		return fmt.Errorf("gate.io cancel order: %w", err)
	}
	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		err := c.cancelRawOrder(ctx, gateSettleUsdt, id)
		if err != nil {
			return fmt.Errorf("gate.io cancel bulk order %s: %w", id, err)
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	err := c.cancelRawAllOpenOrders(ctx, gateSettleUsdt, symbol)
	if err != nil {
		return fmt.Errorf("gate.io cancel all open orders for %s: %w", symbol, err)
	}
	return nil
}

// GetOrder retrieves detailed information about a specific order.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	resp, err := c.getRawOrder(ctx, gateSettleUsdt, orderID)
	if err != nil {
		// Fallback to checking open orders if not found by ID directly
		openOrders, openErr := c.getRawOpenOrders(ctx, gateSettleUsdt, symbol)
		if openErr == nil {
			for i := range openOrders {
				if openOrders[i].Text == orderID || strconv.FormatInt(openOrders[i].Id, 10) == orderID {
					mapped := mapOrderInfo(openOrders[i])
					return &mapped, nil
				}
			}
		}
		return nil, fmt.Errorf("gate.io get order: %w", err)
	}

	mapped := mapOrderInfo(*resp)
	return &mapped, nil
}

// GetOpenOrders retrieves all currently open/active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	resp, err := c.getRawOpenOrders(ctx, gateSettleUsdt, symbol)
	if err != nil {
		return nil, fmt.Errorf("gate.io list open orders for %s: %w", symbol, err)
	}

	orders := make([]exchange.OrderInfo, 0, len(resp))
	for i := range resp {
		orders = append(orders, mapOrderInfo(resp[i]))
	}
	return orders, nil
}

// ClosePosition submits a market order to close an open position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	orderSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		orderSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         orderSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
	})
	if err != nil {
		return fmt.Errorf("gate.io close position: %w", err)
	}
	return nil
}

// CloseAllPositions closes all open positions for the given symbol.
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
			posErr := c.ClosePosition(ctx, symbol, side, pos.HoldVol, domain.PositionModeHedge) // default hedge mode close
			if posErr != nil {
				return posErr
			}
		}
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	var leverageStr string
	var crossLimitStr string

	if req.OpenType == exchange.OpenTypeCross {
		leverageStr = "0"
		crossLimitStr = fmt.Sprintf("%d", req.Leverage)
	} else {
		leverageStr = fmt.Sprintf("%d", req.Leverage)
		crossLimitStr = ""
	}

	return c.changeRawLeverage(ctx, gateSettleUsdt, gateChangeLeverageRequest{
		Symbol:             req.Symbol,
		Leverage:           leverageStr,
		CrossLeverageLimit: crossLimitStr,
	})
}

// SetPositionMode sets the position mode (Hedge Mode vs One-Way Mode) on Gate.io.
func (c *Client) SetPositionMode(ctx context.Context, settle string, dualMode bool) error {
	query := url.Values{}
	query.Set("dual_mode", strconv.FormatBool(dualMode))

	var result struct{}
	path := fmt.Sprintf("/futures/%s/dual_mode", settle)
	return c.sendRequest(ctx, "POST", path, query, nil, &result)
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Gate.io.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	settle := gateSettleUsdt
	modeStr := gateMarginModeIsolated
	if marginMode == gateMarginModeCross {
		modeStr = gateMarginModeCross
	}

	body := gatePositionCrossModeRequest{
		Contract: symbol,
		Mode:     modeStr,
	}

	var result []gatePosition
	path := fmt.Sprintf("/futures/%s/positions/cross_mode", settle)
	err := c.sendRequest(ctx, "POST", path, nil, &body, &result)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/cross_mode", settle)
		var dualResult []gatePosition
		errDual := c.sendRequest(ctx, "POST", dualPath, nil, &body, &dualResult)
		if errDual != nil {
			return fmt.Errorf("gate.io update margin mode error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Helper mapping functions.

func mapOrderPriceAndTif(reqType domain.OrderType, price float64) (priceStr, tif string) {
	if reqType == exchange.OrderTypeMarket {
		return "0", gateTifIOC
	}
	priceStr = decmath.FormatFloat(price)
	switch reqType {
	case exchange.OrderTypePostOnly:
		tif = "poc"
	case exchange.OrderTypeIOC:
		tif = gateTifIOC
	case exchange.OrderTypeFOK:
		tif = "fok"
	default:
		tif = "gtc"
	}
	return priceStr, tif
}

func mapOrderSizeAndClose(side domain.Side, vol float64, positionMode domain.PositionMode, reduceOnly bool) (size int64, autoSize string, isClose bool) {
	isHedge := positionMode == domain.PositionModeHedge
	volInt := int64(vol)

	if isHedge {
		switch side {
		case exchange.SideOpenLong:
			size = volInt
		case exchange.SideOpenShort:
			size = -volInt
		case exchange.SideCloseLong:
			size = 0
			autoSize = "close_long"
		case exchange.SideCloseShort:
			size = 0
			autoSize = "close_short"
		default:
			// SideUnknown or unhandled
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			size = volInt
		case exchange.SideOpenShort, exchange.SideCloseLong:
			size = -volInt
		default:
			// SideUnknown or unhandled
		}
		if reduceOnly {
			isClose = true
		}
	}
	return size, autoSize, isClose
}

// mapSubmitOrder maps a SubmitOrderRequest to gateFuturesOrder.
func (c *Client) mapSubmitOrder(req exchange.SubmitOrderRequest) gateFuturesOrder {
	priceStr, tifVal := mapOrderPriceAndTif(req.Type, req.Price)
	sizeVal, autoSizeVal, closeVal := mapOrderSizeAndClose(req.Side, req.Vol, req.PositionMode, req.ReduceOnly)

	order := gateFuturesOrder{
		Contract: req.Symbol,
		Price:    priceStr,
		Tif:      tifVal,
		Size:     sizeVal,
		AutoSize: autoSizeVal,
		Close:    closeVal,
	}

	if req.ExternalOID != "" {
		order.Text = "t-" + req.ExternalOID
	}

	if req.TakeProfitPrice > 0 {
		order.TpslTpTriggerPrice = decmath.FormatFloat(req.TakeProfitPrice)
		order.TpslTpPriceType = gatePriceTypeLast
	}
	if req.StopLossPrice > 0 {
		order.TpslSlTriggerPrice = decmath.FormatFloat(req.StopLossPrice)
		order.TpslSlPriceType = gatePriceTypeLast
	}

	return order
}

// mapOrderInfo maps a gateFuturesOrder to exchange.OrderInfo.
func mapOrderInfo(raw gateFuturesOrder) exchange.OrderInfo {
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
