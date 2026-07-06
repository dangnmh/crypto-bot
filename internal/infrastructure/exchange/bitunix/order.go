package bitunix

import (
	"context"

	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type bitunixCancelOrderItem struct {
	OrderID  string `json:"orderId,omitempty"`
	ClientID string `json:"clientId,omitempty"`
}

type bitunixCancelOrdersRequest struct {
	Symbol    string                   `json:"symbol"`
	OrderList []bitunixCancelOrderItem `json:"orderList"`
}

type bitunixPlaceOrderResp struct {
	Code int `json:"code"`
	Data struct {
		OrderID string `json:"orderId"`
	} `json:"data"`
	Msg string `json:"msg"`
}

type bitunixPendingOrder struct {
	OrderID     string         `json:"orderId"`
	ClientID    string         `json:"clientId"`
	Symbol      string         `json:"symbol"`
	Side        string         `json:"side"`
	TradeSide   string         `json:"tradeSide"`
	OrderType   string         `json:"orderType"`
	Price       string         `json:"price"`
	Qty         string         `json:"qty"`
	FilledQty   string         `json:"filledQty"`
	TradeQty    string         `json:"tradeQty"`
	DealQty     string         `json:"dealQty"`
	AvgPrice    string         `json:"avgPrice"`
	Status      string         `json:"status"`
	CTime       xjson.FlexTime `json:"ctime"`
	MTime       xjson.FlexTime `json:"mtime"`
	RealizedPnL string         `json:"realizedPNL"`
}

type bitunixPendingOrdersResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		OrderList []bitunixPendingOrder `json:"orderList"`
	} `json:"data"`
}

type bitunixHistoryPosition struct {
	PositionID  string         `json:"positionId"`
	Symbol      string         `json:"symbol"`
	Side        string         `json:"side"`
	MaxQty      string         `json:"maxQty"`
	Qty         string         `json:"qty"`
	EntryPrice  string         `json:"entryPrice"`
	ClosePrice  string         `json:"closePrice"`
	RealizedPNL string         `json:"realizedPNL"`
	Funding     string         `json:"funding"`
	Fee         string         `json:"fee"`
	CTime       xjson.FlexTime `json:"ctime"`
	MTime       xjson.FlexTime `json:"mtime"`
}

type bitunixHistoryPositionsResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		PositionList []bitunixHistoryPosition `json:"positionList"`
	} `json:"data"`
}

func mapOrderStatus(status string) domain.OrderState {
	switch strings.ToUpper(status) {
	case "NEW", "ORDER_NEW", "PENDING":
		return exchange.OrderStateNew
	case "PART_FILLED", "PARTIALLY_FILLED":
		return exchange.OrderStatePartiallyFilled
	case "FILLED", "ORDER_FILLED":
		return exchange.OrderStateFilled
	case "CANCELED", "ORDER_CANCELED", "REJECTED", "EXPIRED", "PART_FILLED_CANCELED":
		return exchange.OrderStateCanceled
	default:
		return exchange.OrderStateNew
	}
}

func mapToDomainSide(side, tradeSide string) domain.Side {
	s := strings.ToUpper(side)
	ts := strings.ToUpper(tradeSide)
	if s == sideBuy {
		if ts == tradeSideClose {
			return domain.SideCloseShort
		}
		return domain.SideOpenLong
	}
	if s == sideSell {
		if ts == tradeSideClose {
			return domain.SideCloseLong
		}
		return domain.SideOpenShort
	}
	return domain.SideUnknown
}

func mapOrderSide(side domain.Side) (string, string, error) {
	switch side {
	case domain.SideOpenLong:
		return sideBuy, tradeSideOpen, nil
	case domain.SideCloseShort:
		return sideBuy, tradeSideClose, nil
	case domain.SideOpenShort:
		return sideSell, tradeSideOpen, nil
	case domain.SideCloseLong:
		return sideSell, tradeSideClose, nil
	case domain.SideUnknown:
		return "", "", fmt.Errorf("unknown order side")
	default:
		return "", "", fmt.Errorf("unsupported order side")
	}
}

func mapOrderType(orderType domain.OrderType) (string, string) {
	switch orderType {
	case domain.OrderTypeLimit:
		return orderTypeLimit, effectGtc
	case domain.OrderTypePostOnly:
		return orderTypeLimit, "POST_ONLY"
	case domain.OrderTypeIOC:
		return orderTypeLimit, "IOC"
	case domain.OrderTypeFOK:
		return orderTypeLimit, "FOK"
	case domain.OrderTypeMarket:
		return orderTypeMarket, ""
	default:
		return orderTypeLimit, effectGtc
	}
}

// CreateOrder places a new futures order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	sideStr, tradeSideStr, err := mapOrderSide(req.Side)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	orderTypeStr, effectStr := mapOrderType(req.Type)

	body := map[string]any{
		paramSymbol:    req.Symbol,
		paramQty:       strconv.FormatFloat(req.Vol, 'f', -1, 64),
		paramSide:      sideStr,
		paramTradeSide: tradeSideStr,
		paramOrderType: orderTypeStr,
	}

	if req.Type != domain.OrderTypeMarket {
		body["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
		body["effect"] = effectStr
	}

	if req.ExternalOID != "" {
		body["clientId"] = req.ExternalOID
	}

	tpslSubmitted := false
	if req.TakeProfitPrice > 0 {
		body["tpPrice"] = strconv.FormatFloat(req.TakeProfitPrice, 'f', -1, 64)
		body["tpStopType"] = triggerTypeLastPrice
		body["tpOrderType"] = orderTypeMarket
		tpslSubmitted = true
	}
	if req.StopLossPrice > 0 {
		body["slPrice"] = strconv.FormatFloat(req.StopLossPrice, 'f', -1, 64)
		body["slStopType"] = triggerTypeLastPrice
		body["slOrderType"] = orderTypeMarket
		tpslSubmitted = true
	}

	resp, err := c.rawPlaceOrder(ctx, body)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	return exchange.CreateOrderResult{
		OrderID:       resp.Data.OrderID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

func (c *Client) rawPlaceOrder(ctx context.Context, body map[string]any) (*bitunixPlaceOrderResp, error) {
	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/trade/place_order", nil, body)
	if err != nil {
		return nil, err
	}

	var resp bitunixPlaceOrderResp
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal place order response: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("place order failed code %d: %s", resp.Code, resp.Msg)
	}

	return &resp, nil
}

// CancelOrder cancels a single order.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if symbol == "" {
		pending, err := c.GetOpenOrders(ctx, "")
		if err == nil {
			for i := range pending {
				if pending[i].OrderID == orderID {
					symbol = pending[i].Symbol
					break
				}
			}
		}
	}

	items := []bitunixCancelOrderItem{{OrderID: orderID}}
	return c.rawCancelOrders(ctx, symbol, items)
}

// CancelOrders cancels multiple orders.
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

	// Group order IDs by symbol
	bySymbol := make(map[string][]string)
	for _, id := range orderIDs {
		sym := orderSyms[id]
		if sym == "" {
			orderInfo, errDetail := c.GetOrder(ctx, "", id)
			if errDetail == nil && orderInfo != nil {
				sym = orderInfo.Symbol
			}
		}
		if sym == "" {
			continue
		}
		bySymbol[sym] = append(bySymbol[sym], id)
	}

	for sym, ids := range bySymbol {
		items := make([]bitunixCancelOrderItem, len(ids))
		for i, id := range ids {
			items[i] = bitunixCancelOrderItem{OrderID: id}
		}

		if err := c.rawCancelOrders(ctx, sym, items); err != nil {
			return err
		}
	}

	return nil
}

func (c *Client) rawCancelOrders(ctx context.Context, symbol string, items []bitunixCancelOrderItem) error {
	req := bitunixCancelOrdersRequest{
		Symbol:    symbol,
		OrderList: items,
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/trade/cancel_orders", nil, req)
	if err != nil {
		return err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal cancel orders response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("cancel orders failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	orders, err := c.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range orders {
		err := c.CancelOrder(ctx, symbol, orders[i].OrderID)
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) toOrderInfoList(data []bitunixPendingOrder) []exchange.OrderInfo {
	orders := make([]exchange.OrderInfo, 0, len(data))
	for i := range data {
		item := &data[i]

		price := decmath.ParseFloat(item.Price)
		qty := decmath.ParseFloat(item.Qty)

		filledQty := decmath.ParseFloat(item.TradeQty)
		if filledQty == 0 {
			filledQty = decmath.ParseFloat(item.FilledQty)
		}
		if filledQty == 0 {
			filledQty = decmath.ParseFloat(item.DealQty)
		}

		avgPrice := decmath.ParseFloat(item.AvgPrice)

		ctime := int64(item.CTime)
		utime := int64(item.MTime)

		side := mapToDomainSide(item.Side, item.TradeSide)

		orders = append(orders, exchange.OrderInfo{
			OrderID:      item.OrderID,
			Symbol:       item.Symbol,
			Price:        price,
			Vol:          qty,
			DealAvgPrice: avgPrice,
			DealVol:      filledQty,
			State:        mapOrderStatus(item.Status),
			ExternalOID:  item.ClientID,
			Side:         side,
			PositionMode: domain.PositionModeHedge,
			CreateTime:   ctime,
			UpdateTime:   utime,
		})
	}
	return orders
}

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	item, err := c.rawGetOrderDetail(ctx, orderID)
	if err != nil {
		// Fallback to checking order history if not found in active orders
		historyOrders, errHist := c.GetHistoryOrders(ctx, symbol, 0, 0, 0, 100)
		if errHist == nil {
			for i := range historyOrders {
				if historyOrders[i].OrderID == orderID {
					return &historyOrders[i], nil
				}
			}
		}
		return nil, err
	}

	price := decmath.ParseFloat(item.Price)
	qty := decmath.ParseFloat(item.Qty)

	filledQty := decmath.ParseFloat(item.TradeQty)
	if filledQty == 0 {
		filledQty = decmath.ParseFloat(item.FilledQty)
	}
	if filledQty == 0 {
		filledQty = decmath.ParseFloat(item.DealQty)
	}

	avgPrice := decmath.ParseFloat(item.AvgPrice)

	ctime := int64(item.CTime)

	utime := int64(item.MTime)

	side := mapToDomainSide(item.Side, item.TradeSide)

	return &exchange.OrderInfo{
		OrderID:      item.OrderID,
		Symbol:       item.Symbol,
		Price:        price,
		Vol:          qty,
		DealAvgPrice: avgPrice,
		DealVol:      filledQty,
		State:        mapOrderStatus(item.Status),
		ExternalOID:  item.ClientID,
		Side:         side,
		PositionMode: domain.PositionModeHedge,
		CreateTime:   ctime,
		UpdateTime:   utime,
	}, nil
}

type bitunixOrderDetail struct {
	OrderID      string         `json:"orderId"`
	ClientID     string         `json:"clientId"`
	Symbol       string         `json:"symbol"`
	Side         string         `json:"side"`
	TradeSide    string         `json:"tradeSide,omitempty"`
	OrderType    string         `json:"orderType"`
	Price        string         `json:"price"`
	Qty          string         `json:"qty"`
	TradeQty     string         `json:"tradeQty"`
	FilledQty    string         `json:"filledQty,omitempty"`
	DealQty      string         `json:"dealQty,omitempty"`
	AvgPrice     string         `json:"avgPrice,omitempty"`
	Status       string         `json:"status"`
	CTime        xjson.FlexTime `json:"ctime"`
	MTime        xjson.FlexTime `json:"mtime"`
	Fee          string         `json:"fee"`
	RealizedPnL  string         `json:"realizedPNL"`
	PositionMode string         `json:"positionMode"`
	MarginMode   string         `json:"marginMode"`
	Leverage     int            `json:"leverage"`
	Effect       string         `json:"effect"`
	ReduceOnly   bool           `json:"reduceOnly"`
	TpPrice      string         `json:"tpPrice"`
	TpStopType   string         `json:"tpStopType"`
	TpOrderType  string         `json:"tpOrderType"`
	TpOrderPrice string         `json:"tpOrderPrice"`
	SlPrice      string         `json:"slPrice"`
	SlStopType   string         `json:"slStopType"`
	SlOrderType  string         `json:"slOrderType"`
	SlOrderPrice string         `json:"slOrderPrice"`
}

func (c *Client) rawGetOrderDetail(ctx context.Context, orderID string) (*bitunixOrderDetail, error) {
	query := map[string]string{
		paramOrderID: orderID,
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_order_detail", query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int                `json:"code"`
		Data bitunixOrderDetail `json:"data"`
		Msg  string             `json:"msg"`
	}
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal order detail: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get order detail failed code %d: %s", resp.Code, resp.Msg)
	}

	return &resp.Data, nil
}

// GetOrderByExternalID queries an order by client ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	pending, err := c.GetOpenOrders(ctx, symbol)
	if err == nil {
		for i := range pending {
			if pending[i].ExternalOID == externalOrderID {
				return &pending[i], nil
			}
		}
	}

	historyOrders, errHist := c.GetHistoryOrders(ctx, symbol, 0, 0, 0, 100)
	if errHist == nil {
		for i := range historyOrders {
			if historyOrders[i].ExternalOID == externalOrderID {
				return &historyOrders[i], nil
			}
		}
	}

	return nil, fmt.Errorf("order not found for external ID %s", externalOrderID)
}

// GetOpenOrders queries all pending orders for a symbol.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	data, err := c.rawGetPendingOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfoList(data), nil
}

func (c *Client) rawGetPendingOrders(ctx context.Context, symbol string) ([]bitunixPendingOrder, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_pending_orders", query, nil)
	if err != nil {
		return nil, err
	}

	var resp bitunixPendingOrdersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pending orders: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get pending orders failed code %d: %s", resp.Code, resp.Msg)
	}

	return resp.Data.OrderList, nil
}

// GetHistoryOrders queries closed/canceled historical orders.
func (c *Client) GetHistoryOrders(ctx context.Context, symbol string, startTime, endTime, skip, limit int64) ([]exchange.OrderInfo, error) {
	data, err := c.rawGetHistoryOrders(ctx, symbol, startTime, endTime, skip, limit)
	if err != nil {
		return nil, err
	}
	return c.toOrderInfoList(data), nil
}

func (c *Client) rawGetHistoryOrders(ctx context.Context, symbol string, startTime, endTime, skip, limit int64) ([]bitunixPendingOrder, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}
	if startTime > 0 {
		query["startTime"] = strconv.FormatInt(startTime, 10)
	}
	if endTime > 0 {
		query["endTime"] = strconv.FormatInt(endTime, 10)
	}
	if skip > 0 {
		query["skip"] = strconv.FormatInt(skip, 10)
	}
	if limit > 0 {
		query["limit"] = strconv.FormatInt(limit, 10)
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/trade/get_history_orders", query, nil)
	if err != nil {
		return nil, err
	}

	var resp bitunixPendingOrdersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal history orders: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get history orders failed code %d: %s", resp.Code, resp.Msg)
	}

	return resp.Data.OrderList, nil
}

func calculatePnLRateAndEntry(exitPrice, totalPnL, totalQty float64, isLong bool) (entryPrice, pnlRate float64) {
	entryPrice = exitPrice
	if totalQty > 0 {
		if isLong {
			entryPrice = exitPrice - (totalPnL / totalQty)
		} else {
			entryPrice = exitPrice + (totalPnL / totalQty)
		}
	}

	if entryPrice > 0 {
		if isLong {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}
	return
}

func matchClosedPosition(orderInfo *exchange.OrderInfo, positions []bitunixHistoryPosition) *bitunixHistoryPosition {
	targetSide := posSideLong
	if orderInfo.Side == domain.SideCloseShort || orderInfo.Side == domain.SideOpenShort {
		targetSide = posSideShort
	}

	for i := range positions {
		p := &positions[i]
		posSide := posSideLong
		if strings.EqualFold(p.Side, posSideShort) || strings.EqualFold(p.Side, "SELL") {
			posSide = posSideShort
		}
		if posSide == targetSide {
			mtimeVal := int64(p.MTime)
			if mtimeVal >= orderInfo.CreateTime {
				return p
			}
		}
	}
	return nil
}

func parseClosedSize(match *bitunixHistoryPosition, orderDealVol float64) float64 {
	closedSize := decmath.ParseFloat(match.MaxQty)
	if closedSize == 0 {
		closedSize = decmath.ParseFloat(match.Qty)
	}
	if closedSize == 0 {
		closedSize = orderDealVol
	}
	return closedSize
}

func parseExitPrice(match *bitunixHistoryPosition, orderDealAvgPrice, orderPrice float64) float64 {
	exitPrice := decmath.ParseFloat(match.ClosePrice)
	if exitPrice == 0 {
		exitPrice = orderDealAvgPrice
	}
	if exitPrice == 0 {
		exitPrice = orderPrice
	}
	return exitPrice
}

func calculatePnLRate(entryPrice, exitPrice float64, isLong bool) float64 {
	if entryPrice <= 0 {
		return 0.0
	}
	if isLong {
		return ((exitPrice - entryPrice) / entryPrice) * 100.0
	}
	return ((entryPrice - exitPrice) / entryPrice) * 100.0
}

func calculateDurationMs(match *bitunixHistoryPosition, orderCreateTime int64) int64 {
	ctimeVal := int64(match.CTime)
	mtimeVal := int64(match.MTime)

	tVal := mtimeVal
	if tVal > ctimeVal {
		return tVal - ctimeVal
	}
	if tVal > orderCreateTime {
		return tVal - orderCreateTime
	}
	return 0
}

// GetOrderPNL queries closed positions history to calculate closed PnL metrics for an order.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("bitunix get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeBitunixName,
			Symbol:   symbol,
		}, nil
	}

	// We query history positions to get closed position metrics.
	// We look for a closed position that matches this order's symbol, direction and close time.
	startTime := max(orderInfo.CreateTime, 0)
	positions, errHist := c.rawGetHistoryPositions(ctx, symbol, startTime)
	if errHist != nil {
		return nil, fmt.Errorf("query history positions: %w", errHist)
	}

	match := matchClosedPosition(orderInfo, positions)
	if match == nil {
		return nil, fmt.Errorf("no matching closed position found for order %s", orderID)
	}

	closedSize := parseClosedSize(match, orderInfo.DealVol)
	netPnL := decmath.ParseFloat(match.RealizedPNL)
	fee := decmath.ParseFloat(match.Fee)
	fundingFee := decmath.ParseFloat(match.Funding)
	grossPnL := netPnL + fee - fundingFee

	exitPrice := parseExitPrice(match, orderInfo.DealAvgPrice, orderInfo.Price)
	isLong := orderInfo.Side == domain.SideCloseLong || orderInfo.Side == domain.SideOpenLong

	entryPrice := decmath.ParseFloat(match.EntryPrice)
	if entryPrice == 0 {
		entryPrice, _ = calculatePnLRateAndEntry(exitPrice, grossPnL, closedSize, isLong)
	}

	pnlRate := calculatePnLRate(entryPrice, exitPrice, isLong)
	durationMs := calculateDurationMs(match, orderInfo.CreateTime)

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeBitunixName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        fee,
		FundingFee: fundingFee,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
		DurationMs: durationMs,
	}, nil
}

func (c *Client) rawGetHistoryPositions(ctx context.Context, symbol string, startTime int64) ([]bitunixHistoryPosition, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	if startTime > 0 {
		query["startTime"] = strconv.FormatInt(startTime, 10)
	}

	histPosBody, err := c.request(ctx, http.MethodGet, "/api/v1/futures/position/get_history_positions", query, nil)
	if err != nil {
		return nil, err
	}

	var resp bitunixHistoryPositionsResponse
	if err := xjson.Unmarshal(histPosBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal history positions: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get history positions failed code %d: %s", resp.Code, resp.Msg)
	}

	return resp.Data.PositionList, nil
}
