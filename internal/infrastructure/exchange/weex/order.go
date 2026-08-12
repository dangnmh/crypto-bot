package weex

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"

	"github.com/google/uuid"
)

type weexOrderResponse struct {
	OrderID       string `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Success       bool   `json:"success"`
	ErrorCode     string `json:"errorCode"`
	ErrorMessage  string `json:"errorMessage"`
}

type weexOrder struct {
	OrderID       xjson.Number `json:"orderId"`
	ClientOrderID string       `json:"clientOrderId"`
	Symbol        string       `json:"symbol"`
	Side          string       `json:"side"`
	PositionSide  string       `json:"positionSide"`
	Type          string       `json:"type"`
	Price         string       `json:"price"`
	OrigQty       string       `json:"origQty"`
	ExecutedQty   string       `json:"executedQty"`
	CumQuote      string       `json:"cumQuote"`
	AvgPrice      string       `json:"avgPrice"`
	Status        string       `json:"status"`
	Time          xjson.Number `json:"time"`
	UpdateTime    xjson.Number `json:"updateTime"`
}

func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	body := c.buildOrderBody(req)

	resBytes, err := c.request(ctx, http.MethodPost, "/capi/v3/order", nil, body, true)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	data, err := parseResponse[weexOrderResponse](resBytes)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	if !data.Success && data.ErrorMessage != "" {
		return exchange.CreateOrderResult{}, fmt.Errorf("WEEX order placement failed: %s (%s)", data.ErrorMessage, data.ErrorCode)
	}

	return exchange.CreateOrderResult{
		OrderID:       data.OrderID,
		TPSLSubmitted: req.TakeProfitPrice > 0 || req.StopLossPrice > 0,
	}, nil
}

func (c *Client) buildOrderBody(req exchange.SubmitOrderRequest) map[string]any {
	clientOid := req.ExternalOID
	if clientOid == "" {
		clientOid = uuid.NewString()
	}

	sideStr := sideBuy
	if req.Side == domain.SideOpenShort || req.Side == domain.SideCloseLong {
		sideStr = sideSell
	}

	posSideStr := posSideLong
	if req.Side == domain.SideOpenShort || req.Side == domain.SideCloseShort {
		posSideStr = posSideShort
	}

	typeStr := typeLimit
	if req.Type == exchange.OrderTypeMarket {
		typeStr = typeMarket
	}

	body := map[string]any{
		keySymbol:          req.Symbol,
		"side":             sideStr,
		"positionSide":     posSideStr,
		"type":             typeStr,
		"quantity":         strconv.FormatFloat(req.Vol, 'f', -1, 64),
		"newClientOrderId": clientOid,
	}

	if req.Type != exchange.OrderTypeMarket {
		body["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
	}

	switch req.Type {
	case exchange.OrderTypePostOnly:
		body["timeInForce"] = "POST_ONLY"
	case exchange.OrderTypeIOC:
		body["timeInForce"] = "IOC"
	case exchange.OrderTypeFOK:
		body["timeInForce"] = "FOK"
	default:
		// Default cases for Limit, Market, etc.
	}

	if req.TakeProfitPrice > 0 {
		body["tpTriggerPrice"] = strconv.FormatFloat(req.TakeProfitPrice, 'f', -1, 64)
	}
	if req.StopLossPrice > 0 {
		body["slTriggerPrice"] = strconv.FormatFloat(req.StopLossPrice, 'f', -1, 64)
	}

	return body
}

func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	query := map[string]string{
		keySymbol:  symbol,
		keyOrderId: orderID,
	}
	resBytes, err := c.request(ctx, http.MethodDelete, "/capi/v3/order", query, nil, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](resBytes)
	return err
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	if len(orderIDs) == 0 {
		return nil
	}
	body := map[string][]string{
		keyOrderIdList: orderIDs,
	}
	resBytes, err := c.request(ctx, http.MethodDelete, "/capi/v3/batchOrders", nil, body, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](resBytes)
	return err
}

func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	query := map[string]string{
		keySymbol: symbol,
	}
	resBytes, err := c.request(ctx, http.MethodDelete, "/capi/v3/allOpenOrders", query, nil, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](resBytes)
	return err
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	query := map[string]string{
		keyOrderId: orderID,
	}
	resBytes, err := c.request(ctx, http.MethodGet, "/capi/v3/order", query, nil, true)
	if err != nil {
		return nil, err
	}
	data, err := parseResponse[weexOrder](resBytes)
	if err != nil {
		return nil, err
	}
	info := c.mapOrder(data)
	return &info, nil
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	info, err := c.findWeexOrder(ctx, symbol, func(o weexOrder) bool {
		return o.ClientOrderID == externalOrderID
	})
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, fmt.Errorf("order not found by external ID: %s", externalOrderID)
	}
	return info, nil
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	query := map[string]string{}
	if symbol != "" {
		query[keySymbol] = symbol
	}
	resBytes, err := c.request(ctx, http.MethodGet, "/capi/v3/openOrders", query, nil, true)
	if err != nil {
		return nil, err
	}
	rawList, err := parseResponse[[]weexOrder](resBytes)
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		orders = append(orders, c.mapOrder(rawList[i]))
	}
	return orders, nil
}

func (c *Client) getRawHistoryOrders(ctx context.Context, symbol string) ([]weexOrder, error) {
	query := map[string]string{
		"limit": "100",
	}
	if symbol != "" {
		query[keySymbol] = symbol
	}
	resBytes, err := c.request(ctx, http.MethodGet, "/capi/v3/order/history", query, nil, true)
	if err != nil {
		return nil, err
	}
	return parseResponse[[]weexOrder](resBytes)
}

func (c *Client) findWeexOrder(ctx context.Context, symbol string, predicate func(weexOrder) bool) (*exchange.OrderInfo, error) {
	openOrders, err := c.GetOpenOrders(ctx, symbol)
	if err == nil {
		for i := range openOrders {
			var statusStr string
			switch openOrders[i].State {
			case exchange.OrderStateFilled:
				statusStr = stateFilled
			case exchange.OrderStatePartiallyFilled:
				statusStr = statePartiallyFilled
			case exchange.OrderStateCanceled:
				statusStr = stateCanceled
			default:
				statusStr = stateNew
			}

			raw := weexOrder{
				OrderID:       xjson.Number(openOrders[i].OrderID),
				ClientOrderID: openOrders[i].ExternalOID,
				Symbol:        openOrders[i].Symbol,
				Price:         strconv.FormatFloat(openOrders[i].Price, 'f', -1, 64),
				OrigQty:       strconv.FormatFloat(openOrders[i].Vol, 'f', -1, 64),
				ExecutedQty:   strconv.FormatFloat(openOrders[i].DealVol, 'f', -1, 64),
				AvgPrice:      strconv.FormatFloat(openOrders[i].DealAvgPrice, 'f', -1, 64),
				Status:        statusStr,
				Time:          xjson.Number(strconv.FormatInt(openOrders[i].CreateTime, 10)),
				UpdateTime:    xjson.Number(strconv.FormatInt(openOrders[i].UpdateTime, 10)),
			}
			if predicate(raw) {
				return &openOrders[i], nil
			}
		}
	}

	history, err := c.getRawHistoryOrders(ctx, symbol)
	if err == nil {
		for i := range history {
			if predicate(history[i]) {
				info := c.mapOrder(history[i])
				return &info, nil
			}
		}
	}

	return nil, nil
}

func (c *Client) mapOrder(o weexOrder) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(o.Price, 64)
	origQty, _ := strconv.ParseFloat(o.OrigQty, 64)
	execQty, _ := strconv.ParseFloat(o.ExecutedQty, 64)
	avgPx, _ := strconv.ParseFloat(o.AvgPrice, 64)
	cTimeVal, _ := o.Time.Int64()
	uTimeVal, _ := o.UpdateTime.Int64()

	state := exchange.OrderStateNew
	switch strings.ToUpper(o.Status) {
	case stateFilled:
		state = exchange.OrderStateFilled
	case statePartiallyFilled, statePartialFill:
		state = exchange.OrderStatePartiallyFilled
	case stateCanceled, stateCancelled:
		state = exchange.OrderStateCanceled
	}

	var side domain.Side
	isBuy := strings.EqualFold(o.Side, "BUY")
	isLong := strings.EqualFold(o.PositionSide, "LONG")

	if isBuy {
		if isLong {
			side = exchange.SideOpenLong
		} else {
			side = exchange.SideCloseShort
		}
	} else {
		if isLong {
			side = exchange.SideCloseLong
		} else {
			side = exchange.SideOpenShort
		}
	}

	return exchange.OrderInfo{
		OrderID:      o.OrderID.String(),
		Symbol:       o.Symbol,
		Price:        px,
		Vol:          origQty,
		DealAvgPrice: avgPx,
		DealVol:      execQty,
		State:        state,
		ExternalOID:  o.ClientOrderID,
		CreateTime:   cTimeVal,
		UpdateTime:   uTimeVal,
		Side:         side,
		PositionMode: domain.PositionModeHedge, // Default Hedge Mode for WEEX
	}
}

type weexIncomeItem struct {
	BillID     xjson.Number `json:"billId"`
	Asset      string       `json:"asset"`
	Symbol     string       `json:"symbol"`
	Income     xjson.Number `json:"income"`
	IncomeType string       `json:"incomeType"`
	Time       xjson.Number `json:"time"`
}

type weexIncomeResponse struct {
	HasNextPage bool             `json:"hasNextPage"`
	Items       []weexIncomeItem `json:"items"`
}

type weexTrade struct {
	ID           xjson.Number `json:"id"`
	OrderID      xjson.Number `json:"orderId"`
	Symbol       string       `json:"symbol"`
	Price        string       `json:"price"`
	Qty          string       `json:"qty"`
	QuoteQty     string       `json:"quoteQty"`
	RealizedPnl  string       `json:"realizedPnl"`
	Commission   string       `json:"commission"`
	Time         int64        `json:"time"`
	Side         string       `json:"side"`
	PositionSide string       `json:"positionSide"`
}

type aggregatedTradeMetrics struct {
	totalQty    float64
	sumPriceQty float64
	totalPnL    float64
	totalFee    float64
	latestTime  int64
	posSide     string
}

// GetOrderPNL queries personal trade logs to calculate closed PnL metrics for an order.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("weex get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
			Status:   orderInfo.State,
		}, nil
	}

	query := map[string]string{
		keySymbol: symbol,
	}
	if orderInfo.CreateTime > 0 {
		query["startTime"] = strconv.FormatInt(orderInfo.CreateTime, 10)
	}
	resBytes, err := c.RawRequest(ctx, http.MethodGet, "/capi/v3/userTrades", query, nil)
	if err != nil {
		return nil, err
	}

	var trades []weexTrade
	trades, err = parseResponse[[]weexTrade](resBytes)
	if err != nil {
		return nil, err
	}

	openMetrics, closeMetrics := aggregateTrades(orderID, trades)
	if closeMetrics.totalQty == 0 {
		return nil, fmt.Errorf("no trades found for order %s", orderID)
	}

	exitPrice := 0.0
	if closeMetrics.totalQty > 0 {
		exitPrice = closeMetrics.sumPriceQty / closeMetrics.totalQty
	}

	entryPrice := calculateEntryPrice(openMetrics, closeMetrics, exitPrice)

	durationMs := int64(0)
	if orderInfo.CreateTime > 0 && closeMetrics.latestTime > orderInfo.CreateTime {
		durationMs = closeMetrics.latestTime - orderInfo.CreateTime
	}

	pnlRate := calculatePnLRate(entryPrice, exitPrice, closeMetrics.posSide)

	fundingFee, _ := c.fetchFundingFee(ctx, symbol, orderInfo.CreateTime)

	return &exchange.ClosedPnLInfo{
		Exchange:           exchangeName,
		Symbol:             symbol,
		Status:             orderInfo.State,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosedSizeContract: new(closeMetrics.totalQty),
		GrossPnL:           closeMetrics.totalPnL,
		Fee:                closeMetrics.totalFee + openMetrics.totalFee,
		FundingFee:         fundingFee,
		NetPnl:             closeMetrics.totalPnL - (closeMetrics.totalFee + openMetrics.totalFee) + fundingFee,
		PnLRate:            pnlRate,
		DurationMs:         durationMs,
	}, nil
}

func calculateEntryPrice(openMetrics, closeMetrics aggregatedTradeMetrics, exitPrice float64) float64 {
	if openMetrics.totalQty > 0 {
		return openMetrics.sumPriceQty / openMetrics.totalQty
	}
	if closeMetrics.totalQty > 0 {
		if strings.EqualFold(closeMetrics.posSide, posSideLong) {
			return exitPrice - (closeMetrics.totalPnL / closeMetrics.totalQty)
		}
		return exitPrice + (closeMetrics.totalPnL / closeMetrics.totalQty)
	}
	return 0.0
}

func calculatePnLRate(entryPrice, exitPrice float64, posSide string) float64 {
	if entryPrice <= 0 {
		return 0.0
	}
	if strings.EqualFold(posSide, posSideLong) {
		return ((exitPrice - entryPrice) / entryPrice) * 100.0
	}
	return ((entryPrice - exitPrice) / entryPrice) * 100.0
}

func (c *Client) fetchFundingFee(ctx context.Context, symbol string, startTime int64) (float64, error) {
	var startVal any
	if startTime > 0 {
		startVal = startTime
	}
	body := map[string]any{
		"asset":        "",
		keySymbol:      symbol,
		keyIncomeType:  valPositionFunding,
		keyStartTime:   startVal,
		keyEndTime:     nil,
		keyLimit:       100,
		keyNextKeyID:   nil,
		keyNextKeyTime: nil,
	}
	resBytes, err := c.request(ctx, http.MethodPost, "/capi/v3/account/income", nil, body, true)
	if err != nil {
		return 0, err
	}
	resp, err := parseResponse[weexIncomeResponse](resBytes)
	if err != nil {
		return 0, err
	}
	var totalFunding float64
	for i := range resp.Items {
		val, _ := resp.Items[i].Income.Float64()
		totalFunding += val
	}
	return totalFunding, nil
}

func aggregateTrades(orderID string, trades []weexTrade) (aggregatedTradeMetrics, aggregatedTradeMetrics) {
	var openMetrics aggregatedTradeMetrics
	var closeMetrics aggregatedTradeMetrics

	// Find the opening trades (matching the orderID)
	for i := range trades {
		t := &trades[i]
		if t.OrderID.String() == orderID {
			qty, _ := strconv.ParseFloat(t.Qty, 64)
			price, _ := strconv.ParseFloat(t.Price, 64)
			fee, _ := strconv.ParseFloat(t.Commission, 64)

			openMetrics.totalQty += qty
			openMetrics.sumPriceQty += price * qty
			openMetrics.totalFee += fee
			if t.Time > openMetrics.latestTime {
				openMetrics.latestTime = t.Time
			}
			openMetrics.posSide = t.PositionSide
		}
	}

	// If no opening trades were found, we can't determine closing trades
	if openMetrics.totalQty == 0 {
		return openMetrics, closeMetrics
	}

	// The closing trade side will be opposite to the opening trade direction
	// If posSide is LONG, the closing side is SELL (short/reducing the long)
	// If posSide is SHORT, the closing side is BUY (long/reducing the short)
	var targetCloseSide string
	if strings.EqualFold(openMetrics.posSide, posSideLong) {
		targetCloseSide = sideSell
	} else {
		targetCloseSide = sideBuy
	}

	// Find the closing trades (different orderID, same positionSide, side matching targetCloseSide, time at or after opening trade)
	for i := range trades {
		t := &trades[i]
		if t.OrderID.String() != orderID &&
			strings.EqualFold(t.PositionSide, openMetrics.posSide) &&
			t.Time >= openMetrics.latestTime &&
			strings.EqualFold(t.Side, targetCloseSide) {
			qty, _ := strconv.ParseFloat(t.Qty, 64)
			price, _ := strconv.ParseFloat(t.Price, 64)
			pnl, _ := strconv.ParseFloat(t.RealizedPnl, 64)
			fee, _ := strconv.ParseFloat(t.Commission, 64)

			closeMetrics.totalQty += qty
			closeMetrics.sumPriceQty += price * qty
			closeMetrics.totalPnL += pnl
			closeMetrics.totalFee += fee
			if t.Time > closeMetrics.latestTime {
				closeMetrics.latestTime = t.Time
			}
			closeMetrics.posSide = t.PositionSide
		}
	}

	return openMetrics, closeMetrics
}
