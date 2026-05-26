package binance

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures/src/restapi/models"
)

// CreateOrder places a new order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	sdkSide := models.NewAlgoOrderSideParameter(sideBuy)
	if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseLong {
		sdkSide = models.NewAlgoOrderSideParameter(sideSell)
	}

	sdkType := orderTypeLimit
	sdkTif := models.NewAlgoOrderTimeInForceParameter("GTC")

	switch req.Type {
	case exchange.OrderTypeMarket:
		sdkType = orderTypeMarket
	case exchange.OrderTypePostOnly:
		sdkType = orderTypeLimit
		sdkTif = models.NewAlgoOrderTimeInForceParameter("GTX")
	case exchange.OrderTypeIOC:
		sdkType = orderTypeLimit
		sdkTif = models.NewAlgoOrderTimeInForceParameter("IOC")
	case exchange.OrderTypeFOK:
		sdkType = orderTypeLimit
		sdkTif = models.NewAlgoOrderTimeInForceParameter("FOK")
	}

	orderReq := c.sdkClient.RestApi.TradeAPI.NewOrder(ctx).
		Symbol(req.Symbol).
		Side(sdkSide).
		Type(sdkType).
		Quantity(float32(req.Vol))

	if sdkType != orderTypeMarket {
		orderReq = orderReq.Price(float32(req.Price)).TimeInForce(sdkTif)
	}

	// Position side for hedge mode
	if req.PositionMode == 1 {
		posSide := models.NewAlgoOrderPositionSideParameter(posSideLong)
		if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseShort {
			posSide = models.NewAlgoOrderPositionSideParameter(posSideShort)
		}
		orderReq = orderReq.PositionSide(posSide)
	}

	if req.ReduceOnly {
		orderReq = orderReq.ReduceOnly("true")
	}

	if req.ExternalOID != "" {
		orderReq = orderReq.NewClientOrderId(req.ExternalOID)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.NewOrderExecute(orderReq)
	if err != nil {
		return "", fmt.Errorf("binance place order: %w", err)
	}

	orderID := strconv.FormatInt(*resp.Data.OrderId, 10)
	return orderID, nil
}

// CreateTrackOrder submits a trailing stop order. Stubbed.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Binance")
}

// CancelOrder cancels an open order.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid orderID type for binance: %w", err)
	}

	req := c.sdkClient.RestApi.TradeAPI.CancelOrder(ctx).
		Symbol(symbol).
		OrderId(id)

	_, err = c.sdkClient.RestApi.TradeAPI.CancelOrderExecute(req)
	if err != nil {
		errMsg := strings.ToLower(err.Error())
		if strings.Contains(errMsg, "-2011") || strings.Contains(errMsg, "unknown order") || strings.Contains(errMsg, "filled") {
			return nil
		}
		return fmt.Errorf("binance cancel order: %w", err)
	}

	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		err := c.CancelOrder(ctx, "", id)
		if err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	req := c.sdkClient.RestApi.TradeAPI.CancelAllOpenOrders(ctx).Symbol(symbol)
	_, err := c.sdkClient.RestApi.TradeAPI.CancelAllOpenOrdersExecute(req)
	if err != nil {
		return fmt.Errorf("binance cancel all open orders: %w", err)
	}
	return nil
}

// GetOrder queries order status.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid orderID for binance: %w", err)
	}

	req := c.sdkClient.RestApi.TradeAPI.QueryOrder(ctx).OrderId(id)
	resp, err := c.sdkClient.RestApi.TradeAPI.QueryOrderExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance query order: %w", err)
	}

	info := mapOrder(resp.Data)
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	req := c.sdkClient.RestApi.TradeAPI.CurrentAllOpenOrders(ctx)
	if symbol != "" {
		req = req.Symbol(symbol)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.CurrentAllOpenOrdersExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance current open orders: %w", err)
	}

	var list []models.AllOrdersResponseInner
	if resp.Data.Items != nil {
		list = resp.Data.Items
	}

	orders := make([]exchange.OrderInfo, 0, len(list))
	for i := range list {
		orders = append(orders, mapAllOrder(list[i]))
	}

	return orders, nil
}

// ClosePosition closes a single position.
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

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := positions[i]
		if pos.HoldVol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == 1 { // Long
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, pos.HoldVol, 1)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	sdkReq := c.sdkClient.RestApi.TradeAPI.ChangeInitialLeverage(ctx).
		Symbol(req.Symbol).
		Leverage(int64(req.Leverage))

	_, err := c.sdkClient.RestApi.TradeAPI.ChangeInitialLeverageExecute(sdkReq)
	if err != nil {
		return fmt.Errorf("binance change leverage: %w", err)
	}
	return nil
}

// mapOrder maps a Binance QueryOrderResponse model to exchange.OrderInfo struct.
//
//nolint:cyclop // standard SDK mapping logic contains branch complexity
func mapOrder(raw models.QueryOrderResponse) exchange.OrderInfo {
	id := ""
	if raw.OrderId != nil {
		id = strconv.FormatInt(*raw.OrderId, 10)
	}

	price := 0.0
	if raw.Price != nil {
		price = decmath.ParseFloat(*raw.Price)
	}

	vol := 0.0
	if raw.OrigQty != nil {
		vol = decmath.ParseFloat(*raw.OrigQty)
	}

	dealAvg := 0.0
	if raw.AvgPrice != nil {
		dealAvg = decmath.ParseFloat(*raw.AvgPrice)
	}

	dealVol := 0.0
	if raw.ExecutedQty != nil {
		dealVol = decmath.ParseFloat(*raw.ExecutedQty)
	}

	clientOID := ""
	if raw.ClientOrderId != nil {
		clientOID = *raw.ClientOrderId
	}

	info := exchange.OrderInfo{
		OrderID:      id,
		Symbol:       raw.GetSymbol(),
		Price:        price,
		Vol:          vol,
		DealAvgPrice: dealAvg,
		DealVol:      dealVol,
		ExternalOID:  clientOID,
		PositionMode: 2, // default OneWay
	}

	switch raw.GetPositionSide() {
	case posSideLong:
		info.Side = exchange.SideOpenLong
		if raw.GetSide() == sideSell {
			info.Side = exchange.SideCloseLong
		}
		info.PositionMode = 1
	case posSideShort:
		info.Side = exchange.SideOpenShort
		if raw.GetSide() == sideBuy {
			info.Side = exchange.SideCloseShort
		}
		info.PositionMode = 1
	default:
		// One-way mode mapping
		if raw.GetSide() == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	// Status mapping
	switch raw.GetStatus() {
	case statusNew:
		info.State = exchange.OrderStatePartial
	case statusPart:
		info.State = exchange.OrderStatePartial
	case statusFilled:
		info.State = exchange.OrderStateFilled
	case statusCancel, statusExpired:
		info.State = exchange.OrderStateCanceled
	}

	if raw.Time != nil {
		info.CreateTime = *raw.Time
	}
	if raw.UpdateTime != nil {
		info.UpdateTime = *raw.UpdateTime
	}

	return info
}

// mapAllOrder maps models.AllOrdersResponseInner to exchange.OrderInfo.
//
//nolint:cyclop // standard SDK mapping logic contains branch complexity
func mapAllOrder(raw models.AllOrdersResponseInner) exchange.OrderInfo {
	id := ""
	if raw.OrderId != nil {
		id = strconv.FormatInt(*raw.OrderId, 10)
	}

	price := 0.0
	if raw.Price != nil {
		price = decmath.ParseFloat(*raw.Price)
	}

	vol := 0.0
	if raw.OrigQty != nil {
		vol = decmath.ParseFloat(*raw.OrigQty)
	}

	dealAvg := 0.0
	if raw.AvgPrice != nil {
		dealAvg = decmath.ParseFloat(*raw.AvgPrice)
	}

	dealVol := 0.0
	if raw.ExecutedQty != nil {
		dealVol = decmath.ParseFloat(*raw.ExecutedQty)
	}

	clientOID := ""
	if raw.ClientOrderId != nil {
		clientOID = *raw.ClientOrderId
	}

	info := exchange.OrderInfo{
		OrderID:      id,
		Symbol:       raw.GetSymbol(),
		Price:        price,
		Vol:          vol,
		DealAvgPrice: dealAvg,
		DealVol:      dealVol,
		ExternalOID:  clientOID,
		PositionMode: 2,
	}

	switch raw.GetPositionSide() {
	case posSideLong:
		info.Side = exchange.SideOpenLong
		if raw.GetSide() == sideSell {
			info.Side = exchange.SideCloseLong
		}
		info.PositionMode = 1
	case posSideShort:
		info.Side = exchange.SideOpenShort
		if raw.GetSide() == sideBuy {
			info.Side = exchange.SideCloseShort
		}
		info.PositionMode = 1
	default:
		if raw.GetSide() == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	switch raw.GetStatus() {
	case "NEW":
		info.State = exchange.OrderStatePartial
	case "PARTIALLY_FILLED":
		info.State = exchange.OrderStatePartial
	case "FILLED":
		info.State = exchange.OrderStateFilled
	case "CANCELED", "EXPIRED":
		info.State = exchange.OrderStateCanceled
	}

	if raw.Time != nil {
		info.CreateTime = *raw.Time
	}
	if raw.UpdateTime != nil {
		info.UpdateTime = *raw.UpdateTime
	}

	return info
}
