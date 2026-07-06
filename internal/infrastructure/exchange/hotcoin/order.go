package hotcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type hotcoinPlaceOrderResponse struct {
	ID xjson.Number `json:"id"`
}

type hotcoinBaseOrder struct {
	ID          int64          `json:"id"`
	Amount      xjson.Number   `json:"amount"`
	DealAmount  xjson.Number   `json:"dealAmount"`
	Price       xjson.Number   `json:"price"`
	AvgPrice    xjson.Number   `json:"avgPrice"`
	Status      int            `json:"status"` // 0: pending, 1: partially filled, 2: fully filled, -1: cancelled
	DetailSide  string         `json:"detailSide"`
	Tag         string         `json:"tag"`
	CreatedDate xjson.FlexTime `json:"createdDate"`
	ModifyDate  xjson.FlexTime `json:"modifyDate"`
}

type hotcoinOrderDetail struct {
	hotcoinBaseOrder
	ContractCode string `json:"contractCode"`
	OrderID      int64  `json:"orderId"`
}

type hotcoinOrderListItem struct {
	hotcoinBaseOrder
	ContractCode string `json:"contractCode"`
}

type hotcoinHistoryResponse struct {
	Code int `json:"code"`
	Data struct {
		Rows []hotcoinOrderListItem `json:"rows"`
	} `json:"data"`
}

type hotcoinDealRecord struct {
	ContractCode string       `json:"contractCode"`
	Amount       xjson.Number `json:"amount"`
	Price        xjson.Number `json:"price"`
	Fee          xjson.Number `json:"fee"`
	Profit       xjson.Number `json:"profit"`
	OrderID      xjson.Number `json:"orderId"`
	RefOrderID   xjson.Number `json:"refOrderId"`
	DetailSide   string       `json:"detailSide"`
	CreateDate   string       `json:"createDate"`
}

type hotcoinDealRecordResponse struct {
	Code int `json:"code"`
	Data struct {
		Data []hotcoinDealRecord `json:"data"`
	} `json:"data"`
}

func mapOrderType(t domain.OrderType) (string, error) {
	switch t {
	case domain.OrderTypeLimit, domain.OrderTypePostOnly, domain.OrderTypeIOC:
		return "10", nil
	case domain.OrderTypeMarket:
		return "11", nil
	default:
		return "", fmt.Errorf("unsupported order type: %v", t)
	}
}

func mapOrderSide(s domain.Side) (string, error) {
	switch s {
	case domain.SideOpenLong:
		return sideOpenLong, nil
	case domain.SideCloseShort:
		return sideCloseShort, nil
	case domain.SideOpenShort:
		return sideOpenShort, nil
	case domain.SideCloseLong:
		return sideCloseLong, nil
	default:
		return "", fmt.Errorf("unsupported side: %v", s)
	}
}

// CreateOrder places a new limit, market, or post-only order.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	contractCode := strings.ToLower(strings.ReplaceAll(req.Symbol, "_", ""))

	typeStr, err := mapOrderType(req.Type)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	sideStr, err := mapOrderSide(req.Side)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	body := map[string]any{
		typeKey:   typeStr,
		sideKey:   sideStr,
		amountKey: int(req.Vol),
	}

	if req.Type == domain.OrderTypeLimit || req.Type == domain.OrderTypePostOnly || req.Type == domain.OrderTypeIOC {
		body["price"] = strconv.FormatFloat(req.Price, 'f', -1, 64)
	}

	if req.Type == domain.OrderTypePostOnly {
		body["beMaker"] = 1
	}

	if req.PositionID > 0 {
		body["positionId"] = req.PositionID
	}

	path := fmt.Sprintf("/api/v1/perpetual/products/%s/order", contractCode)
	bodyBytes, err := c.request(ctx, http.MethodPost, path, nil, body, true)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	var resp hotcoinPlaceOrderResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("unmarshal place order response: %w", err)
	}

	orderIDStr := resp.ID.String()
	if orderIDStr == "" || orderIDStr == "0" {
		return exchange.CreateOrderResult{}, fmt.Errorf("invalid order ID returned")
	}

	if req.Type == domain.OrderTypeIOC {
		_ = c.CancelOrder(ctx, req.Symbol, orderIDStr)
	}

	return exchange.CreateOrderResult{
		OrderID: orderIDStr,
	}, nil
}

// CancelOrder cancels an active order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))

	// 1. Try standard regular order cancel first
	path := fmt.Sprintf("/api/v1/perpetual/products/%s/order/%s", contractCode, orderID)
	_, err := c.request(ctx, http.MethodDelete, path, nil, nil, true)
	if err != nil {
		// 2. If standard cancel fails, try conditional order cancel endpoint
		condPath := fmt.Sprintf("/api/v1/perpetual/products/%s/order/condition/%s", contractCode, orderID)
		if _, condErr := c.request(ctx, http.MethodDelete, condPath, nil, nil, true); condErr == nil {
			return nil
		}
		return err
	}
	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	openOrders, err := c.GetOpenOrders(ctx, "")
	if err != nil {
		return err
	}

	for _, id := range orderIDs {
		for i := range openOrders {
			o := &openOrders[i]
			if o.OrderID == id {
				if err := c.CancelOrder(ctx, o.Symbol, id); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all active open orders (regular and conditional) for a specific symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	path := fmt.Sprintf("/api/v1/perpetual/products/%s/orders", contractCode)
	_, err := c.request(ctx, http.MethodDelete, path, nil, nil, true)

	// Fetch open orders and manually cancel remaining conditional orders if any
	openOrders, getErr := c.GetOpenOrders(ctx, symbol)
	if getErr == nil {
		for i := range openOrders {
			_ = c.CancelOrder(ctx, symbol, openOrders[i].OrderID)
		}
	}

	return err
}

// GetOrder queries order status and details by order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	path := fmt.Sprintf("/api/v1/perpetual/products/%s/orderDetail", contractCode)
	body, err := c.request(ctx, http.MethodGet, path, map[string]string{orderIDKey: orderID}, nil, true)
	if err != nil {
		return nil, err
	}

	var raw hotcoinOrderDetail
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal order info: %w", err)
	}

	if raw.ID == 0 && raw.OrderID != 0 {
		raw.ID = raw.OrderID
	}

	info := c.mapOrder(&raw.hotcoinBaseOrder)
	if symbol != "" {
		info.Symbol = symbol
	}
	return &info, nil
}

// GetOrderByExternalID retrieves an order by its client tag/external OID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	// First scan open orders
	openOrders, err := c.GetOpenOrders(ctx, symbol)
	if err == nil {
		for i := range openOrders {
			if openOrders[i].ExternalOID == externalOrderID {
				return &openOrders[i], nil
			}
		}
	}

	// Scan history orders
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	path := fmt.Sprintf("/api/v1/perpetual/products/%s/history-list", contractCode)
	body, err := c.request(ctx, http.MethodGet, path, map[string]string{pageSizeKey: "50"}, nil, true)
	if err == nil {
		var hist hotcoinHistoryResponse
		if err := json.Unmarshal(body, &hist); err == nil {
			for i := range hist.Data.Rows {
				item := &hist.Data.Rows[i]
				if item.Tag == externalOrderID {
					info := c.mapOrder(&item.hotcoinBaseOrder)
					if symbol != "" {
						info.Symbol = symbol
					}
					return &info, nil
				}
			}
		}
	}

	return nil, nil
}

// GetOpenOrders queries all active open orders (pending or partially filled).
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	if symbol == "" {
		contracts, err := c.GetContractDetails(ctx)
		if err != nil {
			return nil, err
		}
		var allOrders []exchange.OrderInfo
		for i := range contracts {
			details := &contracts[i]
			orders, err := c.GetOpenOrders(ctx, details.Symbol)
			if err == nil {
				allOrders = append(allOrders, orders...)
			}
		}
		return allOrders, nil
	}

	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	path := fmt.Sprintf("/api/v1/perpetual/products/%s/list", contractCode)
	body, err := c.request(ctx, http.MethodGet, path, nil, nil, true)
	if err != nil {
		return nil, err
	}

	var rawList []hotcoinOrderListItem
	if err := json.Unmarshal(body, &rawList); err != nil {
		return nil, fmt.Errorf("unmarshal open orders: %w", err)
	}

	orders := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		raw := &rawList[i]
		info := c.mapOrder(&raw.hotcoinBaseOrder)
		info.Symbol = symbol
		orders = append(orders, info)
	}
	return orders, nil
}

// GetOrderPNL calculates closed PnL metrics for a filled closing order using deal records.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == domain.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	query := map[string]string{
		contractCodeKey: contractCode,
		pageSizeKey:     "100",
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/perpetual/bills/deal-record", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp hotcoinDealRecordResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal deal records: %w", err)
	}

	openDeals, closeDeals := filterOppositeDeals(orderID, contractCode, resp.Data.Data)

	openQty, openSumPriceQty, openFee, _, _ := aggregateDeals(openDeals)
	closeQty, closeSumPriceQty, closeFee, grossPnL, lastDealTimeStr := aggregateDeals(closeDeals)

	res := calculateClosedPnLInfo(orderInfo, openQty, openSumPriceQty, openFee, closeQty, closeSumPriceQty, closeFee, grossPnL, lastDealTimeStr)
	if res == nil {
		return nil, fmt.Errorf("no trades found for order %s", orderID)
	}

	res.Exchange = exchangeName
	res.Symbol = symbol
	return res, nil
}

func calculateClosedPnLInfo(
	orderInfo *exchange.OrderInfo,
	openQty, openSumPriceQty, openFee,
	closeQty, closeSumPriceQty, closeFee,
	grossPnL float64,
	lastDealTimeStr string,
) *exchange.ClosedPnLInfo {
	if openQty == 0 && closeQty > 0 {
		openQty, openSumPriceQty = closeQty, closeSumPriceQty
	} else if closeQty == 0 && openQty > 0 {
		closeQty, closeSumPriceQty = openQty, openSumPriceQty
	}

	if openQty == 0 || closeQty == 0 {
		return nil
	}

	entryPrice := openSumPriceQty / openQty
	exitPrice := closeSumPriceQty / closeQty

	isLong := orderInfo.Side == domain.SideOpenLong || orderInfo.Side == domain.SideCloseLong
	var pnlRate float64
	if entryPrice > 0 {
		diff := exitPrice - entryPrice
		if !isLong {
			diff = entryPrice - exitPrice
		}
		pnlRate = (diff / entryPrice) * 100.0
	}

	durationMs := parseDurationMs(orderInfo.CreateTime, lastDealTimeStr)

	return &exchange.ClosedPnLInfo{
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closeQty,
		GrossPnL:   grossPnL,
		Fee:        openFee + closeFee,
		FundingFee: 0,
		NetPnl:     grossPnL - (openFee + closeFee),
		PnLRate:    pnlRate,
		DurationMs: durationMs,
	}
}

func parseDurationMs(createTime int64, dealTimeStr string) int64 {
	if createTime <= 0 || dealTimeStr == "" {
		return 0
	}
	t, err := time.Parse("2006-01-02 15:04:05", dealTimeStr)
	if err != nil {
		return 0
	}
	dealMs := t.UnixMilli()
	if dealMs > createTime {
		return dealMs - createTime
	}
	return 0
}

func filterOppositeDeals(openOrderID, contractCode string, data []hotcoinDealRecord) ([]hotcoinDealRecord, []hotcoinDealRecord) {
	var openDeals, closeDeals []hotcoinDealRecord
	var closeOrderID string

	openIDVal, _ := strconv.ParseInt(openOrderID, 10, 64)

	for i := range data {
		item := &data[i]
		if item.OrderID.String() == openOrderID {
			openDeals = append(openDeals, *item)
		}
	}

	for i := range data {
		item := &data[i]
		if strings.EqualFold(item.ContractCode, contractCode) && strings.HasPrefix(strings.ToLower(item.DetailSide), "close") {
			itemOrderIDVal := xjson.ToInt64(item.OrderID)
			if openIDVal > 0 && itemOrderIDVal <= openIDVal {
				continue
			}
			if closeOrderID == "" {
				closeOrderID = item.OrderID.String()
			}
			if item.OrderID.String() == closeOrderID {
				closeDeals = append(closeDeals, *item)
			}
		}
	}

	return openDeals, closeDeals
}

func aggregateDeals(deals []hotcoinDealRecord) (qty, sumPriceQty, totalFee, pnl float64, timeStr string) {
	for i := range deals {
		item := &deals[i]
		q := xjson.ToFloat64(item.Amount)
		p := xjson.ToFloat64(item.Price)
		f := xjson.ToFloat64(item.Fee)
		pnlVal := xjson.ToFloat64(item.Profit)

		qty += q
		sumPriceQty += p * q
		totalFee += math.Abs(f)
		pnl += pnlVal
		timeStr = item.CreateDate
	}
	return
}

func (c *Client) mapOrder(o *hotcoinBaseOrder) exchange.OrderInfo {
	cTime := int64(o.CreatedDate)
	uTime := int64(o.ModifyDate)

	state := domain.OrderStateNew
	switch o.Status {
	case 0:
		state = domain.OrderStateNew
	case 1:
		state = domain.OrderStatePartiallyFilled
	case 2:
		state = domain.OrderStateFilled
	case -1:
		state = domain.OrderStateCanceled
	}

	var side domain.Side
	switch strings.ToLower(o.DetailSide) {
	case sideOpenLong:
		side = domain.SideOpenLong
	case sideCloseShort:
		side = domain.SideCloseShort
	case sideOpenShort:
		side = domain.SideOpenShort
	case sideCloseLong:
		side = domain.SideCloseLong
	}

	return exchange.OrderInfo{
		OrderID:      strconv.FormatInt(o.ID, 10),
		Price:        xjson.ToFloat64(o.Price),
		Vol:          xjson.ToFloat64(o.Amount),
		DealAvgPrice: xjson.ToFloat64(o.AvgPrice),
		DealVol:      xjson.ToFloat64(o.DealAmount),
		State:        state,
		ExternalOID:  o.Tag,
		Side:         side,
		PositionMode: domain.PositionModeHedge,
		CreateTime:   cTime,
		UpdateTime:   uTime,
	}
}

// PlaceTPSL places Take Profit and Stop Loss conditional orders on Hotcoin.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	contractCode := strings.ToLower(strings.ReplaceAll(req.Symbol, "_", ""))

	var sideStr string
	switch req.Side {
	case domain.SideOpenLong, domain.SideCloseLong:
		sideStr = "close_long"
	case domain.SideOpenShort, domain.SideCloseShort:
		sideStr = "close_short"
	default:
		return fmt.Errorf("invalid side for TPSL: %v", req.Side)
	}

	currentPrice := 0.0
	tickers, err := c.GetTickers(ctx, req.Symbol)
	if err == nil && len(tickers) > 0 {
		currentPrice = tickers[0].LastPrice
	} else {
		// Fallback to trigger price
		if req.TakeProfitPrice > 0 {
			currentPrice = req.TakeProfitPrice
		} else {
			currentPrice = req.StopLossPrice
		}
	}

	placeOrder := func(triggerPrice float64) error {
		body := map[string]any{
			typeKey:        "12", // Conditional Order
			"triggerBy":    "mark",
			"triggerPrice": strconv.FormatFloat(triggerPrice, 'f', -1, 64),
			sideKey:        sideStr,
			"algoType":     11, // Market Order
			amountKey:      int(req.Volume),
		}
		if currentPrice > 0 {
			body["currentPrice"] = strconv.FormatFloat(currentPrice, 'f', -1, 64)
		}
		path := fmt.Sprintf("/api/v1/perpetual/products/%s/order", contractCode)
		_, err := c.request(ctx, http.MethodPost, path, nil, body, true)
		return err
	}

	if req.TakeProfitPrice > 0 {
		if err := placeOrder(req.TakeProfitPrice); err != nil {
			return fmt.Errorf("place take profit: %w", err)
		}
	}
	if req.StopLossPrice > 0 {
		if err := placeOrder(req.StopLossPrice); err != nil {
			return fmt.Errorf("place stop loss: %w", err)
		}
	}

	return nil
}
