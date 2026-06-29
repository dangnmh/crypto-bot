package orangex

import (
	"context"
	"fmt"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type createOrderResult struct {
	Order struct {
		OrderID string `json:"order_id"`
	} `json:"order"`
}

type orangexOrder struct {
	OrderID           string       `json:"order_id"`
	OrderState        string       `json:"order_state"`
	Amount            xjson.Number `json:"amount"`
	Price             xjson.Number `json:"price"`
	FilledAmount      xjson.Number `json:"filled_amount"`
	AveragePrice      xjson.Number `json:"average_price"`
	CreationTimestamp int64        `json:"creation_timestamp"`
	InstrumentName    string       `json:"instrument_name"`
	Direction         string       `json:"direction"`
	CustomOrderID     string       `json:"custom_order_id"`
}

type userTrade struct {
	Amount         xjson.Number `json:"amount"`
	Direction      string       `json:"direction"`
	Fee            xjson.Number `json:"fee"`
	FeeCoinType    string       `json:"fee_coin_type"`
	InstrumentName string       `json:"instrument_name"`
	OrderID        string       `json:"order_id"`
	Price          xjson.Number `json:"price"`
	Timestamp      int64        `json:"timestamp"`
}

type userTradesEnvelope struct {
	Trades  []userTrade `json:"trades"`
	HasMore bool        `json:"has_more"`
}

func (c *Client) rawPlaceOrder(ctx context.Context, method string, params any) (*createOrderResult, error) {
	resp, err := c.postRPC(ctx, method, method, params, true)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[createOrderResult]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return &envelope.Result, nil
}

func (c *Client) rawGetOrderState(ctx context.Context, orderID, customOrderID string) (*orangexOrder, error) {
	params := map[string]string{}
	if orderID != "" {
		params["order_id"] = orderID
	}
	if customOrderID != "" {
		params["custom_order_id"] = customOrderID
	}
	resp, err := c.postRPC(ctx, "/private/get_order_state", "/private/get_order_state", params, true)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[orangexOrder]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return &envelope.Result, nil
}

func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	var posSide, method string

	switch req.Side {
	case domain.SideOpenLong:
		posSide = posSideLong
		method = pathBuy
	case domain.SideCloseLong:
		posSide = posSideLong
		method = pathSell
	case domain.SideOpenShort:
		posSide = posSideShort
		method = pathSell
	case domain.SideCloseShort:
		posSide = posSideShort
		method = pathBuy
	default:
		return exchange.CreateOrderResult{}, fmt.Errorf("unsupported order side: %v", req.Side)
	}

	orderType := "limit"
	if req.Type == domain.OrderTypeMarket {
		orderType = typeMarket
	}

	params := map[string]any{
		paramInstrument: req.Symbol,
		paramAmount:     req.Vol,
		paramType:       orderType,
		"position_side": posSide,
	}

	if req.Price > 0 {
		params["price"] = req.Price
	}
	if req.Type == domain.OrderTypePostOnly {
		params["post_only"] = true
	}
	if req.ReduceOnly {
		params["reduce_only"] = true
	}
	if req.ExternalOID != "" {
		params["custom_order_id"] = req.ExternalOID
	}
	if req.TakeProfitPrice > 0 {
		params["take_profit_price"] = req.TakeProfitPrice
		params["take_profit_type"] = 2 // last_price
	}
	if req.StopLossPrice > 0 {
		params["stop_loss_price"] = req.StopLossPrice
		params["stop_loss_type"] = 2 // last_price
	}

	res, err := c.rawPlaceOrder(ctx, method, params)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}
	return exchange.CreateOrderResult{
		OrderID: res.Order.OrderID,
	}, nil
}

func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	params := map[string]string{paramOrderID: orderID}
	_, err := c.postRPC(ctx, "/private/cancel", "/private/cancel", params, true)
	return err
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		if err := c.CancelOrder(ctx, "", id); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	params := map[string]string{paramInstrument: symbol}
	_, err := c.postRPC(ctx, "/private/cancel_all_by_instrument", "/private/cancel_all_by_instrument", params, true)
	return err
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	res, err := c.rawGetOrderState(ctx, orderID, "")
	if err != nil {
		return nil, err
	}
	return mapOrder(res), nil
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	res, err := c.rawGetOrderState(ctx, "", externalOrderID)
	if err != nil {
		return nil, err
	}
	return mapOrder(res), nil
}

func mapOrder(o *orangexOrder) *exchange.OrderInfo {
	state := domain.OrderStateNew
	switch o.OrderState {
	case "filled":
		state = domain.OrderStateFilled
	case "canceled":
		state = domain.OrderStateCanceled
	}
	side := domain.SideOpenLong
	if o.Direction == dirSell {
		side = domain.SideOpenShort
	}
	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.InstrumentName,
		Price:        xjson.ToFloat64(o.Price),
		Vol:          xjson.ToFloat64(o.Amount),
		DealVol:      xjson.ToFloat64(o.FilledAmount),
		DealAvgPrice: xjson.ToFloat64(o.AveragePrice),
		State:        state,
		ExternalOID:  o.CustomOrderID,
		Side:         side,
		CreateTime:   o.CreationTimestamp,
	}
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{paramInstrument: symbol}
	resp, err := c.postRPC(ctx, "/private/get_open_orders_by_instrument", "/private/get_open_orders_by_instrument", params, true)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[[]orangexOrder]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	var out []exchange.OrderInfo
	for i := range envelope.Result {
		out = append(out, *mapOrder(&envelope.Result[i]))
	}
	return out, nil
}

func (c *Client) rawGetUserTradesByOrder(ctx context.Context, orderID string) ([]userTrade, error) {
	params := map[string]string{"order_id": orderID}
	resp, err := c.postRPC(ctx, "/private/get_user_trades_by_order", "/private/get_user_trades_by_order", params, true)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[userTradesEnvelope]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return envelope.Result.Trades, nil
}

func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order details: %w", err)
	}

	if orderInfo.State == domain.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	trades, err := c.rawGetUserTradesByOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	totalQty := 0.0
	totalFee := 0.0
	sumPriceQty := 0.0
	lastTime := int64(0)

	for _, t := range trades {
		qty := xjson.ToFloat64(t.Amount)
		price := xjson.ToFloat64(t.Price)
		totalQty += qty
		totalFee += xjson.ToFloat64(t.Fee)
		sumPriceQty += price * qty
		if t.Timestamp > lastTime {
			lastTime = t.Timestamp
		}
	}

	if totalQty == 0 {
		return nil, fmt.Errorf("no trades found for order %s", orderID)
	}

	averagePrice := sumPriceQty / totalQty
	durationMs := int64(0)
	if orderInfo.CreateTime > 0 && lastTime > orderInfo.CreateTime {
		durationMs = lastTime - orderInfo.CreateTime
	}

	// Gross PnL
	grossPnL := 0.0
	netPnL := 0.0 - totalFee
	pnlRate := 0.0

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: averagePrice,
		ExitPrice:  averagePrice,
		ClosedSize: totalQty,
		GrossPnL:   grossPnL,
		Fee:        totalFee,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
		DurationMs: durationMs,
	}, nil
}
