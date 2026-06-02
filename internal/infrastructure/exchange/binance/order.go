package binance

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures/src/restapi/models"
)

// Explicit request/response structs for order endpoints.

type binanceCreateOrderRequest struct {
	Symbol           string
	Side             models.NewAlgoOrderSideParameter
	Type             string
	Quantity         float32
	Price            float32
	TimeInForce      models.NewAlgoOrderTimeInForceParameter
	PositionSide     models.NewAlgoOrderPositionSideParameter
	ReduceOnly       string
	NewClientOrderId string
}

type binancePlaceAlgoOrderRequest struct {
	AlgoType      string
	Symbol        string
	Side          models.NewAlgoOrderSideParameter
	Type          string
	TriggerPrice  float32
	ClosePosition string
	PositionSide  models.NewAlgoOrderPositionSideParameter
}

type binanceCancelOrderRequest struct {
	Symbol  string
	OrderID int64
}

type binanceCancelAllOpenOrdersRequest struct {
	Symbol string
}

type binanceQueryOrderRequest struct {
	Symbol            string
	OrderID           int64
	OrigClientOrderId string
}

type binanceListOpenOrdersRequest struct {
	Symbol string
}

type binanceChangeLeverageRequest struct {
	Symbol   string
	Leverage int64
}

// Private raw methods invoking the Binance SDK.

func (c *Client) createRawOrder(ctx context.Context, req binanceCreateOrderRequest) (*models.NewOrderResponse, error) {
	orderReq := c.sdkClient.RestApi.TradeAPI.NewOrder(ctx).
		Symbol(req.Symbol).
		Side(req.Side).
		Type(req.Type).
		Quantity(req.Quantity)

	if req.Type != orderTypeMarket {
		orderReq = orderReq.Price(req.Price).TimeInForce(req.TimeInForce)
	}

	if string(req.PositionSide) != "" {
		orderReq = orderReq.PositionSide(req.PositionSide)
	}

	if req.ReduceOnly != "" {
		orderReq = orderReq.ReduceOnly(req.ReduceOnly)
	}

	if req.NewClientOrderId != "" {
		orderReq = orderReq.NewClientOrderId(req.NewClientOrderId)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.NewOrderExecute(orderReq)
	if err != nil {
		return nil, fmt.Errorf("binance place order: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) placeRawAlgoOrder(ctx context.Context, req binancePlaceAlgoOrderRequest) (*models.NewAlgoOrderResponse, error) {
	r := c.sdkClient.RestApi.TradeAPI.NewAlgoOrder(ctx).
		AlgoType(req.AlgoType).
		Symbol(req.Symbol).
		Side(req.Side).
		Type(req.Type).
		TriggerPrice(req.TriggerPrice).
		ClosePosition(req.ClosePosition)

	if string(req.PositionSide) != "" {
		r = r.PositionSide(req.PositionSide)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.NewAlgoOrderExecute(r)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req binanceCancelOrderRequest) (*models.CancelOrderResponse, error) {
	r := c.sdkClient.RestApi.TradeAPI.CancelOrder(ctx).
		Symbol(req.Symbol).
		OrderId(req.OrderID)

	resp, err := c.sdkClient.RestApi.TradeAPI.CancelOrderExecute(r)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, req binanceCancelAllOpenOrdersRequest) (*models.CancelAllOpenOrdersResponse, error) {
	r := c.sdkClient.RestApi.TradeAPI.CancelAllOpenOrders(ctx).Symbol(req.Symbol)
	resp, err := c.sdkClient.RestApi.TradeAPI.CancelAllOpenOrdersExecute(r)
	if err != nil {
		return nil, fmt.Errorf("binance cancel all open orders: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) getRawOrder(ctx context.Context, req binanceQueryOrderRequest) (*models.QueryOrderResponse, error) {
	orderReq := c.sdkClient.RestApi.TradeAPI.QueryOrder(ctx).Symbol(req.Symbol)
	if req.OrderID > 0 {
		orderReq = orderReq.OrderId(req.OrderID)
	} else if req.OrigClientOrderId != "" {
		orderReq = orderReq.OrigClientOrderId(req.OrigClientOrderId)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.QueryOrderExecute(orderReq)
	if err != nil {
		return nil, fmt.Errorf("binance query order: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req binanceListOpenOrdersRequest) (*models.CurrentAllOpenOrdersResponse, error) {
	r := c.sdkClient.RestApi.TradeAPI.CurrentAllOpenOrders(ctx)
	if req.Symbol != "" {
		r = r.Symbol(req.Symbol)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.CurrentAllOpenOrdersExecute(r)
	if err != nil {
		return nil, fmt.Errorf("binance current open orders: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, req binanceChangeLeverageRequest) (*models.ChangeInitialLeverageResponse, error) {
	sdkReq := c.sdkClient.RestApi.TradeAPI.ChangeInitialLeverage(ctx).
		Symbol(req.Symbol).
		Leverage(req.Leverage)

	resp, err := c.sdkClient.RestApi.TradeAPI.ChangeInitialLeverageExecute(sdkReq)
	if err != nil {
		return nil, fmt.Errorf("binance change leverage: %w", err)
	}
	return &resp.Data, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder places a new order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	sdkSide := models.NewAlgoOrderSideParameter(sideBuy)
	if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseLong {
		sdkSide = models.NewAlgoOrderSideParameter(sideSell)
	}

	var sdkType string
	var sdkTif models.NewAlgoOrderTimeInForceParameter

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
	default:
		sdkType = orderTypeLimit
		sdkTif = models.NewAlgoOrderTimeInForceParameter("GTC")
	}

	rawReq := binanceCreateOrderRequest{
		Symbol:   req.Symbol,
		Side:     sdkSide,
		Type:     sdkType,
		Quantity: float32(req.Vol),
	}

	if sdkType != orderTypeMarket {
		rawReq.Price = float32(req.Price)
		rawReq.TimeInForce = sdkTif
	}

	// Position side for hedge mode.
	if req.PositionMode == 1 {
		posSide := models.NewAlgoOrderPositionSideParameter(posSideLong)
		if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseShort {
			posSide = models.NewAlgoOrderPositionSideParameter(posSideShort)
		}
		rawReq.PositionSide = posSide
	}

	if req.ReduceOnly {
		rawReq.ReduceOnly = binanceTrueStr
	}

	if req.ExternalOID != "" {
		rawReq.NewClientOrderId = req.ExternalOID
	}

	resp, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	orderID := strconv.FormatInt(*resp.OrderId, 10)
	return exchange.CreateOrderResult{OrderID: orderID, TPSLSubmitted: false}, nil
}

// PlaceTPSL places Take Profit and Stop Loss conditional orders on Binance.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	var tpSide models.NewAlgoOrderSideParameter
	var slSide models.NewAlgoOrderSideParameter
	var tpPosSide models.NewAlgoOrderPositionSideParameter
	var slPosSide models.NewAlgoOrderPositionSideParameter

	switch req.Side {
	case exchange.SideOpenLong:
		tpSide = models.NewAlgoOrderSideParameter(sideSell)
		slSide = models.NewAlgoOrderSideParameter(sideSell)
		if req.PositionMode == 1 {
			tpPosSide = models.NewAlgoOrderPositionSideParameter(posSideLong)
			slPosSide = models.NewAlgoOrderPositionSideParameter(posSideLong)
		}
	case exchange.SideOpenShort:
		tpSide = models.NewAlgoOrderSideParameter(sideBuy)
		slSide = models.NewAlgoOrderSideParameter(sideBuy)
		if req.PositionMode == 1 {
			tpPosSide = models.NewAlgoOrderPositionSideParameter(posSideShort)
			slPosSide = models.NewAlgoOrderPositionSideParameter(posSideShort)
		}
	default:
		return fmt.Errorf("invalid side for TP/SL placement: %d", req.Side)
	}

	if req.TakeProfitPrice > 0 {
		err := c.placeAlgoOrder(ctx, req.Symbol, tpSide, "TAKE_PROFIT_MARKET", req.TakeProfitPrice, req.PositionMode, tpPosSide)
		if err != nil {
			c.logger.ErrorContext(ctx, "Failed to place Take Profit algo order",
				slog.Any("error", err),
				slog.String("symbol", req.Symbol),
				slog.Float64("takeProfitPrice", req.TakeProfitPrice))
			return fmt.Errorf("binance place TP order: %w", err)
		}
		c.logger.InfoContext(ctx, "Successfully placed Take Profit algo order",
			slog.String("symbol", req.Symbol),
			slog.Float64("takeProfitPrice", req.TakeProfitPrice))
	}

	if req.StopLossPrice > 0 {
		err := c.placeAlgoOrder(ctx, req.Symbol, slSide, "STOP_MARKET", req.StopLossPrice, req.PositionMode, slPosSide)
		if err != nil {
			c.logger.ErrorContext(ctx, "Failed to place Stop Loss algo order",
				slog.Any("error", err),
				slog.String("symbol", req.Symbol),
				slog.Float64("stopLossPrice", req.StopLossPrice))
			return fmt.Errorf("binance place SL order: %w", err)
		}
		c.logger.InfoContext(ctx, "Successfully placed Stop Loss algo order",
			slog.String("symbol", req.Symbol),
			slog.Float64("stopLossPrice", req.StopLossPrice))
	}

	return nil
}

func (c *Client) placeAlgoOrder(ctx context.Context, symbol string, side models.NewAlgoOrderSideParameter, algoType string, price float64, positionMode int, posSide models.NewAlgoOrderPositionSideParameter) error {
	rawReq := binancePlaceAlgoOrderRequest{
		AlgoType:      "CONDITIONAL",
		Symbol:        symbol,
		Side:          side,
		Type:          algoType,
		TriggerPrice:  float32(price),
		ClosePosition: binanceTrueStr,
	}

	if positionMode == 1 {
		rawReq.PositionSide = posSide
	}

	_, err := c.placeRawAlgoOrder(ctx, rawReq)
	return err
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

	_, err = c.cancelRawOrder(ctx, binanceCancelOrderRequest{
		Symbol:  symbol,
		OrderID: id,
	})
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
	_, err := c.cancelRawAllOpenOrders(ctx, binanceCancelAllOpenOrdersRequest{
		Symbol: symbol,
	})
	return err
}

// GetOrder queries order status.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	rawReq := binanceQueryOrderRequest{
		Symbol: symbol,
	}

	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		// Non-numeric ID: treat it as client order ID (origClientOrderId).
		rawReq.OrigClientOrderId = orderID
	} else {
		rawReq.OrderID = id
	}

	resp, err := c.getRawOrder(ctx, rawReq)
	if err != nil {
		return nil, err
	}

	info := mapOrder(*resp)
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	resp, err := c.getRawOpenOrders(ctx, binanceListOpenOrdersRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	var list []models.AllOrdersResponseInner
	if resp.Items != nil {
		list = resp.Items
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
	_, err := c.changeRawLeverage(ctx, binanceChangeLeverageRequest{
		Symbol:   req.Symbol,
		Leverage: int64(req.Leverage),
	})
	return err
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
