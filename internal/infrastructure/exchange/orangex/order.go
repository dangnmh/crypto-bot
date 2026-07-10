package orangex

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

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
	PositionSide      string       `json:"position_side"`
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
	state := exchange.OrderStateNew
	switch o.OrderState {
	case "filled":
		state = exchange.OrderStateFilled
	case "canceled":
		state = exchange.OrderStateCanceled
	}
	var side domain.Side
	switch strings.ToUpper(o.PositionSide) {
	case posSideLong:
		if o.Direction == dirBuy {
			side = domain.SideOpenLong
		} else {
			side = domain.SideCloseLong
		}
	case posSideShort:
		if o.Direction == dirSell {
			side = domain.SideOpenShort
		} else {
			side = domain.SideCloseShort
		}
	default:
		// Fallback to existing logic if position_side is not set/unknown
		side = domain.SideOpenLong
		if o.Direction == dirSell {
			side = domain.SideOpenShort
		}
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

func (c *Client) rawGetUserTradesByInstrument(ctx context.Context, symbol string, startTimestamp int64) ([]userTrade, error) {
	params := map[string]any{
		paramInstrument: symbol,
		paramCount:      100,
	}
	if startTimestamp > 0 {
		params["start_timestamp"] = startTimestamp
	}
	resp, err := c.postRPC(ctx, "/private/get_user_trades_by_instrument", "/private/get_user_trades_by_instrument", params, true)
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

type aggregatedTradesResult struct {
	openQty         float64
	openPriceVol    float64
	openFee         float64
	closeQty        float64
	closePriceVol   float64
	closeFee        float64
	closeLatestTime int64
	isLong          bool
}

func matchClosingTrades(closingTrades []userTrade, openQty float64) (closeQty, closePriceVol, closeFee float64, closeLatestTime int64) {
	for _, t := range closingTrades {
		needed := openQty - closeQty
		if needed <= 0 {
			break
		}
		amt := xjson.ToFloat64(t.Amount)
		price := xjson.ToFloat64(t.Price)
		fee := xjson.ToFloat64(t.Fee)

		if amt <= needed {
			closeQty += amt
			closePriceVol += price * amt
			closeFee += fee
			if t.Timestamp > closeLatestTime {
				closeLatestTime = t.Timestamp
			}
		} else {
			closePriceVol += price * needed
			closeFee += fee * (needed / amt)
			closeQty = openQty
			if t.Timestamp > closeLatestTime {
				closeLatestTime = t.Timestamp
			}
			break
		}
	}
	return
}

func aggregateTrades(orderInfo *exchange.OrderInfo, trades []userTrade) (aggregatedTradesResult, error) {
	var openTrades []userTrade
	var potentialCloseTrades []userTrade

	for _, t := range trades {
		if t.OrderID == orderInfo.OrderID {
			openTrades = append(openTrades, t)
		} else {
			potentialCloseTrades = append(potentialCloseTrades, t)
		}
	}

	if len(openTrades) == 0 {
		return aggregatedTradesResult{}, fmt.Errorf("no opening trades found for order %s", orderInfo.OrderID)
	}

	var res aggregatedTradesResult
	openDir := openTrades[0].Direction
	res.isLong = (openDir == dirBuy)

	var openLatestTime int64
	for _, t := range openTrades {
		amt := xjson.ToFloat64(t.Amount)
		price := xjson.ToFloat64(t.Price)
		fee := xjson.ToFloat64(t.Fee)
		res.openQty += amt
		res.openPriceVol += price * amt
		res.openFee += fee
		if t.Timestamp > openLatestTime {
			openLatestTime = t.Timestamp
		}
	}

	closeDir := dirSell
	if openDir == dirSell {
		closeDir = dirBuy
	}

	switch orderInfo.Side {
	case domain.SideOpenLong:
		closeDir = dirSell
	case domain.SideOpenShort:
		closeDir = dirBuy
	case domain.SideCloseLong, domain.SideCloseShort, domain.SideUnknown:
		// Not opening sides
	}

	var closingTrades []userTrade
	for _, t := range potentialCloseTrades {
		isCloseTrade := (t.Direction == closeDir)
		if isCloseTrade && t.Timestamp >= openLatestTime {
			closingTrades = append(closingTrades, t)
		}
	}

	sort.Slice(closingTrades, func(i, j int) bool {
		return closingTrades[i].Timestamp < closingTrades[j].Timestamp
	})

	res.closeQty, res.closePriceVol, res.closeFee, res.closeLatestTime = matchClosingTrades(closingTrades, res.openQty)

	if res.closeQty == 0 {
		return aggregatedTradesResult{}, fmt.Errorf("no closing trades found for symbol %s and opening order %s", orderInfo.Symbol, orderInfo.OrderID)
	}

	return res, nil
}

func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order details: %w", err)
	}

	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	trades, err := c.rawGetUserTradesByInstrument(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		return nil, err
	}

	agg, err := aggregateTrades(orderInfo, trades)
	if err != nil {
		return nil, err
	}

	entryPrice := agg.openPriceVol / agg.openQty
	exitPrice := agg.closePriceVol / agg.closeQty
	closedSize := agg.closeQty
	totalFee := agg.openFee + agg.closeFee

	grossPnL := 0.0
	if agg.isLong {
		grossPnL = (exitPrice - entryPrice) * closedSize
	} else {
		grossPnL = (entryPrice - exitPrice) * closedSize
	}
	fundingFee, err := c.getFundingFee(ctx, orderInfo.CreateTime)
	if err != nil {
		c.logger.Debug("Failed to fetch funding fee from transaction log", "error", err)
		fundingFee = 0.0
	}
	netPnL := grossPnL - totalFee - fundingFee

	pnlRate := 0.0
	if entryPrice > 0 {
		if agg.isLong {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	durationMs := int64(0)
	if agg.closeLatestTime > orderInfo.CreateTime {
		durationMs = agg.closeLatestTime - orderInfo.CreateTime
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        totalFee,
		FundingFee: fundingFee,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
		DurationMs: durationMs,
	}, nil
}

type orangexLogItem struct {
	ID         string       `json:"id"`
	Type       string       `json:"type"`
	Change     xjson.Number `json:"change"`
	CoinType   string       `json:"coin_type"`
	AssetType  string       `json:"asset_type"`
	CreateTime xjson.Number `json:"create_time"`
}

type orangexTransactionLogResult struct {
	Total int              `json:"total"`
	Logs  []orangexLogItem `json:"logs"`
}

type orangexTransactionLogResponse struct {
	JsonRpc string                      `json:"jsonrpc"`
	Result  orangexTransactionLogResult `json:"result"`
}

func (c *Client) getFundingFee(ctx context.Context, orderCreateTime int64) (float64, error) {
	if orderCreateTime == 0 {
		return 0, nil
	}

	params := map[string]string{
		paramCurrency: "USDT",
		"start_time":  strconv.FormatInt(orderCreateTime, 10),
		paramCount:    "50",
	}

	bodyBytes, err := c.GetTransactionLogRaw(ctx, params)
	if err != nil {
		return 0, fmt.Errorf("fetch transaction log: %w", err)
	}

	var resp orangexTransactionLogResponse
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal transaction log response: %w", err)
	}

	var totalFunding float64
	for _, item := range resp.Result.Logs {
		if strings.EqualFold(item.Type, "perpetual_funding") {
			val := xjson.ToFloat64(item.Change)
			// Negate the value because negative change in logs (payment) matches
			// positive in internal sign convention (TotalFee/HoldFee)
			totalFunding += -val
		}
	}

	return totalFunding, nil
}
