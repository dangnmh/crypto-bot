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
	symbol := "BTCUSDT"
	idStr := orderID
	if sym, id, ok := strings.Cut(orderID, ":"); ok {
		symbol = sym
		idStr = id
	}

	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid orderID for binance: %w", err)
	}

	req := c.sdkClient.RestApi.TradeAPI.QueryOrder(ctx).Symbol(symbol).OrderId(id)
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
func mapOrder(raw models.QueryOrderResponse) exchange.OrderInfo {
	return mapBinanceOrder(&raw)
}

// mapAllOrder maps models.AllOrdersResponseInner to exchange.OrderInfo.
func mapAllOrder(raw models.AllOrdersResponseInner) exchange.OrderInfo {
	return mapBinanceOrder(&raw)
}

type binanceOrderModel interface {
	GetOrderIdOk() (*int64, bool)
	GetPriceOk() (*string, bool)
	GetOrigQtyOk() (*string, bool)
	GetAvgPriceOk() (*string, bool)
	GetExecutedQtyOk() (*string, bool)
	GetClientOrderIdOk() (*string, bool)
	GetPositionSide() string
	GetSide() string
	GetSymbol() string
	GetStatus() string
	GetTimeOk() (*int64, bool)
	GetUpdateTimeOk() (*int64, bool)
}

func mapBinanceOrder(raw binanceOrderModel) exchange.OrderInfo {
	id := ""
	if orderID, ok := raw.GetOrderIdOk(); ok {
		id = strconv.FormatInt(*orderID, 10)
	}

	price := 0.0
	if rawPrice, ok := raw.GetPriceOk(); ok {
		price = decmath.ParseFloat(*rawPrice)
	}

	vol := 0.0
	if rawVol, ok := raw.GetOrigQtyOk(); ok {
		vol = decmath.ParseFloat(*rawVol)
	}

	dealAvg := 0.0
	if rawAvgPrice, ok := raw.GetAvgPriceOk(); ok {
		dealAvg = decmath.ParseFloat(*rawAvgPrice)
	}

	dealVol := 0.0
	if rawExecutedQty, ok := raw.GetExecutedQtyOk(); ok {
		dealVol = decmath.ParseFloat(*rawExecutedQty)
	}

	clientOID := ""
	if rawClientOID, ok := raw.GetClientOrderIdOk(); ok {
		clientOID = *rawClientOID
	}

	side, posMode := mapBinanceSideAndMode(raw.GetPositionSide(), raw.GetSide())

	info := exchange.OrderInfo{
		OrderID:      id,
		Symbol:       raw.GetSymbol(),
		Price:        price,
		Vol:          vol,
		DealAvgPrice: dealAvg,
		DealVol:      dealVol,
		ExternalOID:  clientOID,
		Side:         side,
		PositionMode: posMode,
		State:        mapBinanceStatus(raw.GetStatus()),
	}

	if createTime, ok := raw.GetTimeOk(); ok {
		info.CreateTime = *createTime
	}
	if updateTime, ok := raw.GetUpdateTimeOk(); ok {
		info.UpdateTime = *updateTime
	}

	return info
}

func mapBinanceSideAndMode(positionSide, side string) (int, int) {
	posMode := 2 // default OneWay
	var orderSide int

	switch positionSide {
	case posSideLong:
		orderSide = exchange.SideOpenLong
		if side == sideSell {
			orderSide = exchange.SideCloseLong
		}
		posMode = 1
	case posSideShort:
		orderSide = exchange.SideOpenShort
		if side == sideBuy {
			orderSide = exchange.SideCloseShort
		}
		posMode = 1
	default:
		if side == sideBuy {
			orderSide = exchange.SideOpenLong
		} else {
			orderSide = exchange.SideOpenShort
		}
	}
	return orderSide, posMode
}

func mapBinanceStatus(status string) int {
	switch status {
	case statusNew:
		return exchange.OrderStatePartial
	case statusPart:
		return exchange.OrderStatePartial
	case statusFilled:
		return exchange.OrderStateFilled
	case statusCancel, statusExpired:
		return exchange.OrderStateCanceled
	default:
		return exchange.OrderStatePartial
	}
}
