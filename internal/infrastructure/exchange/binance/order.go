package binance

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for order endpoints.

type binanceCreateOrderRequest struct {
	Symbol           string
	Side             string
	Type             string
	Quantity         float64
	Price            float64
	TimeInForce      string
	PositionSide     string
	ReduceOnly       string
	NewClientOrderId string
}

type binancePlaceAlgoOrderRequest struct {
	AlgoType      string
	Symbol        string
	Side          string
	Type          string
	TriggerPrice  float64
	ClosePosition string
	PositionSide  string
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

// Private raw methods invoking the Binance API directly.

func (c *Client) createRawOrder(ctx context.Context, req binanceCreateOrderRequest) (*binanceOrder, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["side"] = req.Side
	params["type"] = req.Type
	params["quantity"] = req.Quantity

	if req.Type != orderTypeMarket {
		params["price"] = req.Price
		params["timeInForce"] = req.TimeInForce
	}

	if req.PositionSide != "" {
		params["positionSide"] = req.PositionSide
	}

	if req.ReduceOnly != "" {
		params["reduceOnly"] = req.ReduceOnly
	}

	if req.NewClientOrderId != "" {
		params["newClientOrderId"] = req.NewClientOrderId
	}

	var resp binanceOrder
	err := c.request(ctx, http.MethodPost, "/fapi/v1/order", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance place order: %w", err)
	}
	return &resp, nil
}

func (c *Client) placeRawAlgoOrder(ctx context.Context, req binancePlaceAlgoOrderRequest) (*binanceOrder, error) {
	params := make(map[string]any)
	params["algoType"] = req.AlgoType
	params["symbol"] = req.Symbol
	params["side"] = req.Side
	params["type"] = req.Type
	params["triggerPrice"] = req.TriggerPrice
	params["closePosition"] = req.ClosePosition

	if req.PositionSide != "" {
		params["positionSide"] = req.PositionSide
	}

	var resp binanceOrder
	err := c.request(ctx, http.MethodPost, "/fapi/v1/algoOrder", params, true, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req binanceCancelOrderRequest) (*binanceOrder, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["orderId"] = req.OrderID

	var resp binanceOrder
	err := c.request(ctx, http.MethodDelete, "/fapi/v1/order", params, true, &resp)
	if err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, req binanceCancelAllOpenOrdersRequest) (any, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol

	var resp any
	err := c.request(ctx, http.MethodDelete, "/fapi/v1/allOpenOrders", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance cancel all open orders: %w", err)
	}
	return resp, nil
}

func (c *Client) getRawOrder(ctx context.Context, req binanceQueryOrderRequest) (*binanceOrder, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	if req.OrderID > 0 {
		params["orderId"] = req.OrderID
	} else if req.OrigClientOrderId != "" {
		params["origClientOrderId"] = req.OrigClientOrderId
	}

	var resp binanceOrder
	err := c.request(ctx, http.MethodGet, "/fapi/v1/order", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance query order: %w", err)
	}
	return &resp, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req binanceListOpenOrdersRequest) ([]binanceOrder, error) {
	params := make(map[string]any)
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}

	var resp []binanceOrder
	err := c.request(ctx, http.MethodGet, "/fapi/v1/openOrders", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance current open orders: %w", err)
	}
	return resp, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, req binanceChangeLeverageRequest) (*changeLeverageResponse, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["leverage"] = req.Leverage

	var resp changeLeverageResponse
	err := c.request(ctx, http.MethodPost, "/fapi/v1/leverage", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance change leverage: %w", err)
	}
	return &resp, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder places a new order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	sdkSide := sideBuy
	if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseLong {
		sdkSide = sideSell
	}

	var sdkType string
	var sdkTif string

	switch req.Type {
	case exchange.OrderTypeMarket:
		sdkType = orderTypeMarket
	case exchange.OrderTypePostOnly:
		sdkType = orderTypeLimit
		sdkTif = "GTX"
	case exchange.OrderTypeIOC:
		sdkType = orderTypeLimit
		sdkTif = "IOC"
	case exchange.OrderTypeFOK:
		sdkType = orderTypeLimit
		sdkTif = "FOK"
	default:
		sdkType = orderTypeLimit
		sdkTif = "GTC"
	}

	rawReq := binanceCreateOrderRequest{
		Symbol:   req.Symbol,
		Side:     sdkSide,
		Type:     sdkType,
		Quantity: req.Vol,
	}

	if sdkType != orderTypeMarket {
		rawReq.Price = req.Price
		rawReq.TimeInForce = sdkTif
	}

	// Position side for hedge mode.
	if req.PositionMode == 1 {
		posSide := posSideLong
		if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseShort {
			posSide = posSideShort
		}
		rawReq.PositionSide = posSide
	} else if req.ReduceOnly {
		rawReq.ReduceOnly = binanceTrueStr
	}

	if req.ExternalOID != "" {
		rawReq.NewClientOrderId = req.ExternalOID
	}

	resp, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	orderID := strconv.FormatInt(resp.OrderId, 10)
	return exchange.CreateOrderResult{OrderID: orderID, TPSLSubmitted: false}, nil
}

// PlaceTPSL places Take Profit and Stop Loss conditional orders on Binance.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	var tpSide string
	var slSide string
	var tpPosSide string
	var slPosSide string

	switch req.Side {
	case exchange.SideOpenLong:
		tpSide = sideSell
		slSide = sideSell
		if req.PositionMode == 1 {
			tpPosSide = posSideLong
			slPosSide = posSideLong
		}
	case exchange.SideOpenShort:
		tpSide = sideBuy
		slSide = sideBuy
		if req.PositionMode == 1 {
			tpPosSide = posSideShort
			slPosSide = posSideShort
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

func (c *Client) placeAlgoOrder(ctx context.Context, symbol, side, algoType string, price float64, positionMode domain.PositionMode, posSide string) error {
	rawReq := binancePlaceAlgoOrderRequest{
		AlgoType:      "CONDITIONAL",
		Symbol:        symbol,
		Side:          side,
		Type:          algoType,
		TriggerPrice:  price,
		ClosePosition: binanceTrueStr,
	}

	if positionMode == domain.PositionModeHedge {
		rawReq.PositionSide = posSide
	}

	_, err := c.placeRawAlgoOrder(ctx, rawReq)
	return err
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
		if apiErr, ok := exchange.IsAPIError(err); ok && (apiErr.Code == -2011 || strings.Contains(strings.ToLower(apiErr.Message), "unknown order") || strings.Contains(strings.ToLower(apiErr.Message), "filled")) {
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

// GetOrder queries order status by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	id, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("binance invalid order ID format %q: %w", orderID, err)
	}

	resp, err := c.getRawOrder(ctx, binanceQueryOrderRequest{
		Symbol:  symbol,
		OrderID: id,
	})
	if err != nil {
		return nil, err
	}

	info := mapBinanceOrder(resp)
	return &info, nil
}

// GetOrderByExternalID queries order status by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	resp, err := c.getRawOrder(ctx, binanceQueryOrderRequest{
		Symbol:            symbol,
		OrigClientOrderId: externalOrderID,
	})
	if err != nil {
		return nil, err
	}

	info := mapBinanceOrder(resp)
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

	orders := make([]exchange.OrderInfo, 0, len(resp))
	for i := range resp {
		orders = append(orders, mapBinanceOrder(&resp[i]))
	}

	return orders, nil
}

// ClosePosition closes a single position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "binance"),
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
			if pos.PositionType == exchange.PositionTypeLong {
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, pos.HoldVol, domain.PositionModeHedge, pos.Leverage)
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

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Binance.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	var mode = "ISOLATED"
	if marginMode == "CROSS" {
		mode = "CROSSED"
	}

	params := make(map[string]any)
	params["symbol"] = symbol
	params["marginType"] = mode

	err := c.request(ctx, http.MethodPost, "/fapi/v1/marginType", params, true, nil)
	if err != nil {
		if apiErr, ok := exchange.IsAPIError(err); ok && (apiErr.Code == -4046 || strings.Contains(strings.ToLower(apiErr.Message), "no need to change")) {
			return nil
		}
		return fmt.Errorf("binance switch margin mode: %w", err)
	}
	return nil
}

func mapBinanceOrder(raw *binanceOrder) exchange.OrderInfo {
	id := ""
	if raw.OrderId > 0 {
		id = strconv.FormatInt(raw.OrderId, 10)
	}
	price := decmath.ParseFloat(raw.Price)
	vol := decmath.ParseFloat(raw.OrigQty)
	dealAvg := decmath.ParseFloat(raw.AvgPrice)
	dealVol := decmath.ParseFloat(raw.ExecutedQty)

	side, posMode := mapBinanceSideAndMode(raw.PositionSide, raw.Side)

	info := exchange.OrderInfo{
		OrderID:      id,
		Symbol:       raw.Symbol,
		Price:        price,
		Vol:          vol,
		DealAvgPrice: dealAvg,
		DealVol:      dealVol,
		ExternalOID:  raw.ClientOrderId,
		Side:         side,
		PositionMode: posMode,
		State:        mapBinanceStatus(raw.Status),
	}

	if raw.Time != nil {
		info.CreateTime = *raw.Time
	}
	if raw.UpdateTime != nil {
		info.UpdateTime = *raw.UpdateTime
	}

	return info
}

func mapBinanceSideAndMode(positionSide, side string) (domain.Side, domain.PositionMode) {
	posMode := domain.PositionModeOneWay
	var orderSide domain.Side

	switch positionSide {
	case posSideLong:
		orderSide = exchange.SideOpenLong
		if side == sideSell {
			orderSide = exchange.SideCloseLong
		}
		posMode = domain.PositionModeHedge
	case posSideShort:
		orderSide = exchange.SideOpenShort
		if side == sideBuy {
			orderSide = exchange.SideCloseShort
		}
		posMode = domain.PositionModeHedge
	default:
		if side == sideBuy {
			orderSide = exchange.SideOpenLong
		} else {
			orderSide = exchange.SideOpenShort
		}
	}
	return orderSide, posMode
}

func mapBinanceStatus(status string) domain.OrderState {
	switch status {
	case statusNew:
		return exchange.OrderStateNew
	case statusPart:
		return exchange.OrderStatePartiallyFilled
	case statusFilled:
		return exchange.OrderStateFilled
	case statusCancel, statusExpired:
		return exchange.OrderStateCanceled
	default:
		return exchange.OrderStateNew
	}
}
