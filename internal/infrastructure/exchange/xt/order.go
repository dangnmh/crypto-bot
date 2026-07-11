package xt

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type xtCreateOrderReq struct {
	Symbol        string  `json:"symbol"`
	PositionSide  string  `json:"positionSide"`
	OrderSide     string  `json:"orderSide"`
	OrderType     string  `json:"orderType"`
	OrigQty       int64   `json:"origQty"`
	Price         float64 `json:"price,omitempty"`
	ClientOrderId string  `json:"clientOrderId,omitempty"`
	TimeInForce   string  `json:"timeInForce,omitempty"`
	ReduceOnly    bool    `json:"reduceOnly,omitempty"`
}

type xtCreateOrderResponse struct {
	ReturnCode int64        `json:"returnCode"`
	MsgInfo    string       `json:"msgInfo"`
	Result     xjson.Number `json:"result"`
}

type xtCancelReq struct {
	Symbol  string `json:"symbol"`
	OrderID string `json:"orderId"`
}

type xtCancelResponse struct {
	ReturnCode int64  `json:"returnCode"`
	MsgInfo    string `json:"msgInfo"`
}

type xtOpenOrderInfo struct {
	OrderID       string       `json:"orderId"`
	ClientOrderId string       `json:"clientOrderId"`
	Symbol        string       `json:"symbol"`
	OrderSide     string       `json:"orderSide"`
	PositionSide  string       `json:"positionSide"`
	OrderType     string       `json:"orderType"`
	Price         xjson.Number `json:"price"`
	OrigQty       xjson.Number `json:"origQty"`
	ExecutedQty   xjson.Number `json:"executedQty"`
	AvgPrice      xjson.Number `json:"avgPrice"`
	State         string       `json:"state"`
	CreatedTime   int64        `json:"createdTime"`
	UpdateTime    int64        `json:"updateTime"`
}

type xtOrderDetailInfo struct {
	OrderID       string       `json:"orderId"`
	ClientOrderId string       `json:"clientOrderId"`
	Symbol        string       `json:"symbol"`
	OrderSide     string       `json:"orderSide"`
	PositionSide  string       `json:"positionSide"`
	OrderType     string       `json:"orderType"`
	Price         xjson.Number `json:"price"`
	OrigQty       xjson.Number `json:"origQty"`
	ExecutedQty   xjson.Number `json:"executedQty"`
	AvgPrice      xjson.Number `json:"avgPrice"`
	State         string       `json:"state"`
	CreatedTime   int64        `json:"createdTime"`
	UpdateTime    int64        `json:"updateTime"`
}

type xtHistoryOrderInfo struct {
	OrderID       string       `json:"orderId"`
	ClientOrderId string       `json:"clientOrderId"`
	Symbol        string       `json:"symbol"`
	OrderSide     string       `json:"orderSide"`
	PositionSide  string       `json:"positionSide"`
	OrderType     string       `json:"orderType"`
	Price         xjson.Number `json:"price"`
	OrigQty       xjson.Number `json:"origQty"`
	ExecutedQty   xjson.Number `json:"executedQty"`
	AvgPrice      xjson.Number `json:"avgPrice"`
	State         string       `json:"state"`
	CreatedTime   int64        `json:"createdTime"`
	UpdateTime    int64        `json:"updateTime"`
}

type xtOrderListResponse struct {
	ReturnCode int64             `json:"returnCode"`
	MsgInfo    string            `json:"msgInfo"`
	Result     []xtOpenOrderInfo `json:"result"`
}

type xtOrderHistoryResult struct {
	HasNext bool                 `json:"hasNext"`
	HasPrev bool                 `json:"hasPrev"`
	Items   []xtHistoryOrderInfo `json:"items"`
}

type xtOrderHistoryResponse struct {
	ReturnCode int64                `json:"returnCode"`
	MsgInfo    string               `json:"msgInfo"`
	Result     xtOrderHistoryResult `json:"result"`
}

type xtOrderDetailResponse struct {
	ReturnCode int64             `json:"returnCode"`
	MsgInfo    string            `json:"msgInfo"`
	Result     xtOrderDetailInfo `json:"result"`
}

func mapOrderStatus(status string) domain.OrderState {
	switch strings.ToUpper(status) {
	case "NEW", "PENDING":
		return exchange.OrderStateNew
	case "PARTIALLY_FILLED":
		return exchange.OrderStatePartiallyFilled
	case "FILLED":
		return exchange.OrderStateFilled
	case "CANCELED", "REJECTED", "EXPIRED":
		return exchange.OrderStateCanceled
	default:
		return exchange.OrderStateNew
	}
}

func mapToDomainSide(orderSide, positionSide string) domain.Side {
	os := strings.ToUpper(orderSide)
	ps := strings.ToUpper(positionSide)
	if ps == sideLong {
		if os == sideBuy {
			return domain.SideOpenLong
		}
		return domain.SideCloseLong
	}
	if os == sideSell {
		return domain.SideOpenShort
	}
	return domain.SideCloseShort
}

func mapToExchangeSide(side domain.Side) (orderSide, positionSide string) {
	switch side {
	case domain.SideOpenLong:
		return sideBuy, sideLong
	case domain.SideCloseLong:
		return sideSell, sideLong
	case domain.SideOpenShort:
		return sideSell, sideShort
	case domain.SideCloseShort:
		return sideBuy, sideShort
	default:
		return sideBuy, sideLong
	}
}

func mapOpenOrderToDomain(o *xtOpenOrderInfo) exchange.OrderInfo {
	return exchange.OrderInfo{
		OrderID:      o.OrderID,
		ExternalOID:  o.ClientOrderId,
		Symbol:       toStandardSymbol(o.Symbol),
		Side:         mapToDomainSide(o.OrderSide, o.PositionSide),
		Price:        xjson.ToFloat64(o.Price),
		Vol:          xjson.ToFloat64(o.OrigQty),
		DealVol:      xjson.ToFloat64(o.ExecutedQty),
		DealAvgPrice: xjson.ToFloat64(o.AvgPrice),
		State:        mapOrderStatus(o.State),
		CreateTime:   o.CreatedTime,
		UpdateTime:   o.UpdateTime,
	}
}

func mapOrderDetailToDomain(o *xtOrderDetailInfo) exchange.OrderInfo {
	return exchange.OrderInfo{
		OrderID:      o.OrderID,
		ExternalOID:  o.ClientOrderId,
		Symbol:       toStandardSymbol(o.Symbol),
		Side:         mapToDomainSide(o.OrderSide, o.PositionSide),
		Price:        xjson.ToFloat64(o.Price),
		Vol:          xjson.ToFloat64(o.OrigQty),
		DealVol:      xjson.ToFloat64(o.ExecutedQty),
		DealAvgPrice: xjson.ToFloat64(o.AvgPrice),
		State:        mapOrderStatus(o.State),
		CreateTime:   o.CreatedTime,
		UpdateTime:   o.UpdateTime,
	}
}

func mapHistoryOrderToDomain(o *xtHistoryOrderInfo) exchange.OrderInfo {
	return exchange.OrderInfo{
		OrderID:      o.OrderID,
		ExternalOID:  o.ClientOrderId,
		Symbol:       toStandardSymbol(o.Symbol),
		Side:         mapToDomainSide(o.OrderSide, o.PositionSide),
		Price:        xjson.ToFloat64(o.Price),
		Vol:          xjson.ToFloat64(o.OrigQty),
		DealVol:      xjson.ToFloat64(o.ExecutedQty),
		DealAvgPrice: xjson.ToFloat64(o.AvgPrice),
		State:        mapOrderStatus(o.State),
		CreateTime:   o.CreatedTime,
		UpdateTime:   o.UpdateTime,
	}
}

// Private raw methods.
func (c *Client) rawCreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	os, ps := mapToExchangeSide(req.Side)
	var ot string
	switch req.Type {
	case domain.OrderTypeMarket:
		ot = "MARKET"
	default:
		ot = "LIMIT"
	}

	sym := cleanXTSymbol(req.Symbol)

	payload := xtCreateOrderReq{
		Symbol:        sym,
		PositionSide:  ps,
		OrderSide:     os,
		OrderType:     ot,
		OrigQty:       int64(req.Vol),
		Price:         req.Price,
		ClientOrderId: req.ExternalOID,
		ReduceOnly:    req.ReduceOnly,
	}

	switch req.Type {
	case domain.OrderTypePostOnly:
		payload.TimeInForce = "GTX"
	case domain.OrderTypeIOC:
		payload.TimeInForce = "IOC"
	case domain.OrderTypeFOK:
		payload.TimeInForce = "FOK"
	default:
		// Other order types do not require special timeInForce mappings.
	}

	bodyBytes, err := xjson.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal create order request: %w", err)
	}

	respBytes, err := c.request(ctx, "POST", "/future/trade/v1/order/create", nil, bodyBytes, true)
	if err != nil {
		return "", err
	}

	var resp xtCreateOrderResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return "", fmt.Errorf("unmarshal create order: %w", err)
	}

	if resp.ReturnCode != 0 {
		return "", fmt.Errorf("create order API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return resp.Result.String(), nil
}

func (c *Client) rawCancelOrder(ctx context.Context, symbol, orderID string) error {
	sym := cleanXTSymbol(symbol)

	payload := xtCancelReq{
		Symbol:  sym,
		OrderID: orderID,
	}

	bodyBytes, err := xjson.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal cancel order request: %w", err)
	}

	respBytes, err := c.request(ctx, "POST", "/future/trade/v1/order/cancel", nil, bodyBytes, true)
	if err != nil {
		return err
	}

	var resp xtCancelResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal cancel order response: %w", err)
	}

	if resp.ReturnCode != 0 {
		return fmt.Errorf("cancel order API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return nil
}

// CreateOrder satisfies the Client interface.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	res, err := c.rawCreateOrder(ctx, req)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}
	return exchange.CreateOrderResult{
		OrderID: res,
	}, nil
}

// CancelOrder satisfies the Client interface.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	return c.rawCancelOrder(ctx, symbol, orderID)
}

// CancelOrders satisfies the Client interface.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	if len(orderIDs) == 0 {
		return nil
	}

	pending, err := c.GetOpenOrders(ctx, "")
	if err != nil {
		return fmt.Errorf("fetch pending orders to cancel: %w", err)
	}

	orderSyms := make(map[string]string)
	for i := range pending {
		orderSyms[pending[i].OrderID] = pending[i].Symbol
	}

	for _, oID := range orderIDs {
		sym := orderSyms[oID]
		if sym == "" {
			continue
		}
		_ = c.CancelOrder(ctx, sym, oID)
	}

	return nil
}

// CancelAllOpenOrders satisfies the Client interface.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	sym := cleanXTSymbol(symbol)

	type cancelAllReq struct {
		Symbol string `json:"symbol,omitempty"`
	}

	bodyBytes, err := xjson.Marshal(cancelAllReq{Symbol: sym})
	if err != nil {
		return fmt.Errorf("marshal cancel all request: %w", err)
	}

	respBytes, err := c.request(ctx, "POST", "/future/trade/v1/order/cancel-all", nil, bodyBytes, true)
	if err != nil {
		return err
	}

	var resp xtCancelResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal cancel all response: %w", err)
	}

	if resp.ReturnCode != 0 {
		return fmt.Errorf("cancel all open orders error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return nil
}

// GetOrder satisfies the Client interface.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	sym := cleanXTSymbol(symbol)

	query := map[string]string{
		paramSymbol:  sym,
		paramOrderId: orderID,
	}

	respBytes, err := c.request(ctx, "GET", "/future/trade/v1/order/detail", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp xtOrderDetailResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal order detail: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("get order error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	domainOrder := mapOrderDetailToDomain(&resp.Result)
	return &domainOrder, nil
}

// GetOrderByExternalID satisfies the Client interface.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	pending, err := c.GetOpenOrders(ctx, symbol)
	if err == nil {
		for i := range pending {
			if pending[i].ExternalOID == externalOrderID {
				return &pending[i], nil
			}
		}
	}

	historyOrders, errHist := c.GetHistoryOrders(ctx, symbol, 100)
	if errHist == nil {
		for i := range historyOrders {
			if historyOrders[i].ExternalOID == externalOrderID {
				return &historyOrders[i], nil
			}
		}
	}

	return nil, fmt.Errorf("order not found for external ID %s", externalOrderID)
}

// GetOpenOrders satisfies the Client interface.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	query := make(map[string]string)
	if symbol != "" {
		sym := cleanXTSymbol(symbol)
		query["symbol"] = sym
	}

	respBytes, err := c.request(ctx, "GET", "/future/trade/v1/order/list", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp xtOrderListResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal open orders list: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("get open orders error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	var results []exchange.OrderInfo
	for i := range resp.Result {
		results = append(results, mapOpenOrderToDomain(&resp.Result[i]))
	}

	return results, nil
}

// GetHistoryOrders queries closed/filled historical orders.
func (c *Client) GetHistoryOrders(ctx context.Context, symbol string, limit int) ([]exchange.OrderInfo, error) {
	query := map[string]string{
		"direction": "NEXT",
	}
	if symbol != "" {
		sym := cleanXTSymbol(symbol)
		query["symbol"] = sym
	}
	if limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}

	respBytes, err := c.request(ctx, "GET", "/future/trade/v1/order/list-history", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp xtOrderHistoryResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal history orders list: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("get history orders error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	var results []exchange.OrderInfo
	for i := range resp.Result.Items {
		results = append(results, mapHistoryOrderToDomain(&resp.Result.Items[i]))
	}

	return results, nil
}

// GetOrderPNL satisfies the ClosedPnLProvider interface.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("fetch order: %w", err)
	}

	sym := cleanXTSymbol(symbol)

	var expectedPosSide string
	switch orderInfo.Side {
	case domain.SideCloseLong, domain.SideOpenLong:
		expectedPosSide = sideLong
	case domain.SideCloseShort, domain.SideOpenShort:
		expectedPosSide = sideShort
	default:
		expectedPosSide = sideLong
	}

	matchedItem, err := c.fetchMatchedPositionHistory(ctx, sym, orderInfo.CreateTime, expectedPosSide)
	if err != nil {
		return nil, err
	}

	entryPrice := xjson.ToFloat64(matchedItem.CloseOpenPrice)
	exitPrice := xjson.ToFloat64(matchedItem.ClosePrice)
	grossPnL := xjson.ToFloat64(matchedItem.CloseProfit)
	fee := xjson.ToFloat64(matchedItem.TotalFee)
	closedSize := xjson.ToFloat64(matchedItem.ClosePositionSize)
	fundingFee := xjson.ToFloat64(matchedItem.TotalFundFee)

	flowFundingFee, found, err := c.getFundingFee(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		c.logger.Error("Failed to fetch funding fee from bills, falling back to position history", "error", err)
	}
	if found {
		fundingFee = flowFundingFee
	}

	pnlRate := 0.0
	if entryPrice > 0 {
		isLong := expectedPosSide == sideLong
		if isLong {
			pnlRate = (exitPrice - entryPrice) / entryPrice
		} else {
			pnlRate = (entryPrice - exitPrice) / entryPrice
		}
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   "xt",
		Symbol:     toStandardSymbol(symbol),
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        fee,
		FundingFee: fundingFee,
		DurationMs: matchedItem.CloseTime - matchedItem.OpenTime,
		NetPnl:     grossPnL + fee + fundingFee,
		PnLRate:    pnlRate,
	}, nil
}

func (c *Client) fetchMatchedPositionHistory(ctx context.Context, sym string, createTime int64, expectedSide string) (*xtHistoryPosition, error) {
	posHistoryBytes, err := c.request(ctx, "GET", "/future/trade/v1/position/list-history", map[string]string{
		paramSymbol:    sym,
		paramStartTime: strconv.FormatInt(createTime, 10),
		paramLimit:     "50",
	}, nil, true)
	if err != nil {
		return nil, fmt.Errorf("fetch position history: %w", err)
	}

	var posHistoryResp xtPositionHistoryResponse
	if err := xjson.Unmarshal(posHistoryBytes, &posHistoryResp); err != nil {
		return nil, fmt.Errorf("unmarshal position history: %w", err)
	}
	if posHistoryResp.ReturnCode != 0 {
		return nil, fmt.Errorf("position history API error: code=%d, msg=%s", posHistoryResp.ReturnCode, posHistoryResp.MsgInfo)
	}

	var matchedItem *xtHistoryPosition
	var minDiff int64 = 1<<63 - 1

	for i := range posHistoryResp.Result.Items {
		item := &posHistoryResp.Result.Items[i]
		if !strings.EqualFold(item.PositionSide, expectedSide) {
			continue
		}
		diff := absInt64(item.CloseTime - createTime)
		if diff < minDiff {
			minDiff = diff
			matchedItem = item
		}
	}

	if matchedItem == nil || minDiff > 300000 { // 5 minutes max difference
		return nil, fmt.Errorf("no matching position history found for symbol %s side %s near time %d", sym, expectedSide, createTime)
	}

	return matchedItem, nil
}

func absInt64(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func (c *Client) getFundingFee(ctx context.Context, symbol string, orderCreateTime int64) (float64, bool, error) {
	if orderCreateTime == 0 {
		return 0, false, nil
	}

	sym := cleanXTSymbol(symbol)

	params := map[string]string{
		paramSymbol:    sym,
		paramStartTime: strconv.FormatInt(orderCreateTime, 10),
		"endTime":      strconv.FormatInt(orderCreateTime+60000, 10),
		paramLimit:     "50",
	}

	bodyBytes, err := c.GetBalanceBillsRaw(ctx, params)
	if err != nil {
		return 0, false, fmt.Errorf("fetch balance bills: %w", err)
	}

	var resp xtBalanceBillsResponse
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return 0, false, fmt.Errorf("unmarshal balance bills response: %w", err)
	}

	if resp.ReturnCode != 0 {
		return 0, false, fmt.Errorf("balance bills API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	for _, item := range resp.Result.Items {
		if strings.EqualFold(item.Type, "FUND") && strings.EqualFold(toStandardSymbol(item.Symbol), toStandardSymbol(symbol)) {
			val, err := item.Amount.Float64()
			if err != nil {
				return 0, false, fmt.Errorf("parse amount: %w", err)
			}

			return -math.Abs(val), true, nil
		}
	}

	return 0, false, nil
}
