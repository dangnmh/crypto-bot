package aster

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"

	"github.com/google/uuid"
)

type asterOrderResult struct {
	OrderID       xjson.Number `json:"orderId"`
	ClientOrderID string       `json:"clientOrderId"`
	Status        string       `json:"status"`
	Symbol        string       `json:"symbol"`
	AvgPrice      string       `json:"avgPrice"`
	OrigQty       string       `json:"origQty"`
	ExecutedQty   string       `json:"executedQty"`
	Price         string       `json:"price"`
	UpdateTime    int64        `json:"updateTime"`
}

type asterUserTrade struct {
	OrderId      xjson.Number `json:"orderId"`
	Symbol       string       `json:"symbol"`
	Price        string       `json:"price"`
	Qty          string       `json:"qty"`
	RealizedPnl  string       `json:"realizedPnl"`
	Side         string       `json:"side"`
	PositionSide string       `json:"positionSide"`
	Commission   string       `json:"commission"`
	Time         int64        `json:"time"`
}

type asterIncome struct {
	Symbol     string `json:"symbol"`
	IncomeType string `json:"incomeType"`
	Income     string `json:"income"`
	Time       int64  `json:"time"`
}

// Raw methods.
func (c *Client) rawCreateOrder(ctx context.Context, params map[string]string) (*asterOrderResult, error) {
	body, err := c.request(ctx, http.MethodPost, "/fapi/v3/order", params, true)
	if err != nil {
		return nil, err
	}
	var resp asterOrderResult
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) rawCancelOrder(ctx context.Context, params map[string]string) (*asterOrderResult, error) {
	body, err := c.request(ctx, http.MethodDelete, "/fapi/v3/order", params, true)
	if err != nil {
		return nil, err
	}
	var resp asterOrderResult
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, symbol string) error {
	params := map[string]string{paramSymbol: symbol}
	_, err := c.request(ctx, http.MethodDelete, "/fapi/v3/allOpenOrders", params, true)
	return err
}

func (c *Client) rawGetOrder(ctx context.Context, params map[string]string) (*asterOrderResult, error) {
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/order", params, true)
	if err != nil {
		return nil, err
	}
	var resp asterOrderResult
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *Client) rawGetOpenOrders(ctx context.Context, symbol string) ([]asterOrderResult, error) {
	params := map[string]string{paramSymbol: symbol}
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/openOrders", params, true)
	if err != nil {
		return nil, err
	}
	var resp []asterOrderResult
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) rawGetUserTrades(ctx context.Context, symbol string, startTime int64) ([]asterUserTrade, error) {
	params := map[string]string{paramSymbol: symbol}
	if startTime > 0 {
		params["startTime"] = strconv.FormatInt(startTime, 10)
	}
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/userTrades", params, true)
	if err != nil {
		return nil, err
	}
	var resp []asterUserTrade
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) rawGetIncome(ctx context.Context, symbol, incomeType string, startTime int64) ([]asterIncome, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}
	if incomeType != "" {
		params["incomeType"] = incomeType
	}
	if startTime > 0 {
		params["startTime"] = strconv.FormatInt(startTime, 10)
	}
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/income", params, true)
	if err != nil {
		return nil, err
	}
	var resp []asterIncome
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

// CreateOrder places a new order on Aster V3.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
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
	if req.Type == domain.OrderTypeMarket {
		typeStr = typeMarket
	}

	params := map[string]string{
		paramSymbol:        req.Symbol,
		paramSide:          sideStr,
		paramPositionSide:  posSideStr,
		paramType:          typeStr,
		paramQuantity:      decmath.FormatFloat(req.Vol),
		"newClientOrderId": clientOid,
		"newOrderRespType": "ACK",
	}

	if req.Type != domain.OrderTypeMarket {
		params["price"] = decmath.FormatFloat(req.Price)
		switch req.Type {
		case domain.OrderTypePostOnly:
			params["timeInForce"] = timeInForceGTX
		case domain.OrderTypeIOC:
			params["timeInForce"] = timeInForceIOC
		case domain.OrderTypeFOK:
			params["timeInForce"] = timeInForceFOK
		default:
			params["timeInForce"] = timeInForceGTC
		}
	}

	res, err := c.rawCreateOrder(ctx, params)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	return exchange.CreateOrderResult{
		OrderID:       res.OrderID.String(),
		TPSLSubmitted: false,
	}, nil
}

// CancelOrder cancels an existing order on Aster V3.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	params := map[string]string{
		paramSymbol:  symbol,
		paramOrderId: orderID,
	}
	_, err := c.rawCancelOrder(ctx, params)
	return err
}

// CancelOrders cancels multiple orders on Aster V3.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		sym := ""
		if len(id) > 19 {
			sym = strings.TrimPrefix(id[19:], "ASTER")
		}
		if err := c.CancelOrder(ctx, sym, id); err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a specific symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	return c.rawCancelAllOpenOrders(ctx, symbol)
}

// GetOrder fetches details of a specific order from Aster V3.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	res, err := c.rawGetOrder(ctx, map[string]string{paramSymbol: symbol, paramOrderId: orderID})
	if err != nil {
		return nil, err
	}
	qty, _ := strconv.ParseFloat(res.OrigQty, 64)
	cum, _ := strconv.ParseFloat(res.ExecutedQty, 64)
	price, _ := strconv.ParseFloat(res.Price, 64)
	avgPrice, _ := strconv.ParseFloat(res.AvgPrice, 64)

	state := domain.OrderStateNew
	switch res.Status {
	case statusFilled:
		state = domain.OrderStateFilled
	case statusPartiallyFilled:
		state = domain.OrderStatePartiallyFilled
	case statusCanceled:
		state = domain.OrderStateCanceled
	}

	return &exchange.OrderInfo{
		OrderID:      res.OrderID.String(),
		ExternalOID:  res.ClientOrderID,
		Symbol:       res.Symbol,
		Price:        price,
		DealAvgPrice: avgPrice,
		Vol:          qty,
		DealVol:      cum,
		State:        state,
		UpdateTime:   res.UpdateTime,
	}, nil
}

// GetOrderByExternalID fetches details of an order using client-supplied order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	res, err := c.rawGetOrder(ctx, map[string]string{paramSymbol: symbol, "origClientOrderId": externalOrderID})
	if err != nil {
		return nil, err
	}
	qty, _ := strconv.ParseFloat(res.OrigQty, 64)
	cum, _ := strconv.ParseFloat(res.ExecutedQty, 64)
	price, _ := strconv.ParseFloat(res.Price, 64)
	avgPrice, _ := strconv.ParseFloat(res.AvgPrice, 64)

	state := domain.OrderStateNew
	switch res.Status {
	case statusFilled:
		state = domain.OrderStateFilled
	case statusPartiallyFilled:
		state = domain.OrderStatePartiallyFilled
	case statusCanceled:
		state = domain.OrderStateCanceled
	}

	return &exchange.OrderInfo{
		OrderID:      res.OrderID.String(),
		ExternalOID:  res.ClientOrderID,
		Symbol:       res.Symbol,
		Price:        price,
		DealAvgPrice: avgPrice,
		Vol:          qty,
		DealVol:      cum,
		State:        state,
		UpdateTime:   res.UpdateTime,
	}, nil
}

// GetOpenOrders lists active open orders for a specific symbol on Aster V3.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	res, err := c.rawGetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	orders := make([]exchange.OrderInfo, 0, len(res))
	for i := range res {
		item := &res[i]
		qty, _ := strconv.ParseFloat(item.OrigQty, 64)
		cum, _ := strconv.ParseFloat(item.ExecutedQty, 64)
		price, _ := strconv.ParseFloat(item.Price, 64)
		avgPrice, _ := strconv.ParseFloat(item.AvgPrice, 64)

		orders = append(orders, exchange.OrderInfo{
			OrderID:      item.OrderID.String(),
			Symbol:       item.Symbol,
			Price:        price,
			DealAvgPrice: avgPrice,
			Vol:          qty,
			DealVol:      cum,
			State:        domain.OrderStateNew,
			UpdateTime:   item.UpdateTime,
		})
	}
	return orders, nil
}

// PlaceTPSL sets Take Profit and Stop Loss order rules on Aster V3.
func (c *Client) PlaceTPSL(ctx context.Context, req exchange.TPSLRequest) error {
	sideStr := sideSell
	if req.Side == domain.SideCloseShort {
		sideStr = sideBuy
	}

	posSideStr := posSideLong
	if req.Side == domain.SideCloseShort {
		posSideStr = posSideShort
	}

	if req.TakeProfitPrice > 0 {
		params := map[string]string{
			paramSymbol:        req.Symbol,
			paramSide:          sideStr,
			paramPositionSide:  posSideStr,
			paramType:          typeTakeProfitMarket,
			paramStopPrice:     decmath.FormatFloat(req.TakeProfitPrice),
			paramClosePosition: valTrue,
		}
		_, err := c.rawCreateOrder(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to place TP: %w", err)
		}
	}

	if req.StopLossPrice > 0 {
		params := map[string]string{
			paramSymbol:        req.Symbol,
			paramSide:          sideStr,
			paramPositionSide:  posSideStr,
			paramType:          typeStopMarket,
			paramStopPrice:     decmath.FormatFloat(req.StopLossPrice),
			paramClosePosition: valTrue,
		}
		_, err := c.rawCreateOrder(ctx, params)
		if err != nil {
			return fmt.Errorf("failed to place SL: %w", err)
		}
	}

	return nil
}

func parseEntryTrades(trades []asterUserTrade, orderID string) (priceSum, qtySum, feeSum float64, latestTime int64, posSide string) {
	for i := range trades {
		trade := &trades[i]
		if trade.OrderId.String() == orderID {
			p, _ := strconv.ParseFloat(trade.Price, 64)
			q, _ := strconv.ParseFloat(trade.Qty, 64)
			fee, _ := strconv.ParseFloat(trade.Commission, 64)

			priceSum += p * q
			qtySum += q
			feeSum += fee
			if trade.Time > latestTime {
				latestTime = trade.Time
			}
			posSide = trade.PositionSide
		}
	}
	return
}

func parseExitTrades(trades []asterUserTrade, orderID, posSide string, entryLatestTime int64) (priceSum, qtySum, grossPnl, feeSum float64, latestTime int64) {
	targetCloseSide := sideSell
	if strings.EqualFold(posSide, posSideShort) {
		targetCloseSide = sideBuy
	}
	latestTime = entryLatestTime

	for i := range trades {
		trade := &trades[i]
		if trade.OrderId.String() != orderID &&
			strings.EqualFold(trade.PositionSide, posSide) &&
			trade.Time >= entryLatestTime &&
			strings.EqualFold(trade.Side, targetCloseSide) {
			p, _ := strconv.ParseFloat(trade.Price, 64)
			q, _ := strconv.ParseFloat(trade.Qty, 64)
			pnl, _ := strconv.ParseFloat(trade.RealizedPnl, 64)
			fee, _ := strconv.ParseFloat(trade.Commission, 64)

			priceSum += p * q
			qtySum += q
			grossPnl += pnl
			feeSum += fee
			if trade.Time > latestTime {
				latestTime = trade.Time
			}
		}
	}
	return
}

// GetOrderPNL tracks closed trade realized profits, fee, and funding fee aggregate metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("query order detail: %w", err)
	}

	trades, err := c.rawGetUserTrades(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("query trades: %w", err)
	}

	entryPriceSum, entryQtySum, totalFee1, latestTime1, posSide := parseEntryTrades(trades, orderID)
	if entryQtySum == 0 {
		return nil, fmt.Errorf("no entry trades found for order %s", orderID)
	}

	exitPriceSum, exitQtySum, grossPnl, totalFee2, latestTime2 := parseExitTrades(trades, orderID, posSide, latestTime1)

	entryPrice := entryPriceSum / entryQtySum
	exitPrice := entryPrice
	if exitQtySum > 0 {
		exitPrice = exitPriceSum / exitQtySum
	}

	latestTime := max(latestTime2, latestTime1)

	durationMs := int64(0)
	if orderInfo.UpdateTime > 0 && latestTime > orderInfo.UpdateTime {
		durationMs = latestTime - orderInfo.UpdateTime
	}

	pnlRate := 0.0
	if entryPrice > 0 {
		if strings.EqualFold(posSide, posSideLong) {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	fundingFee, _ := c.fetchFundingFee(ctx, symbol, orderInfo.CreateTime)

	totalFee := totalFee1 + totalFee2

	return &exchange.ClosedPnLInfo{
		Exchange:   "aster",
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: entryQtySum,
		GrossPnL:   grossPnl,
		Fee:        totalFee,
		FundingFee: fundingFee,
		NetPnl:     grossPnl - totalFee + fundingFee,
		PnLRate:    pnlRate,
		DurationMs: durationMs,
	}, nil
}

func (c *Client) fetchFundingFee(ctx context.Context, symbol string, startTime int64) (float64, error) {
	incomes, err := c.rawGetIncome(ctx, symbol, "FUNDING_FEE", startTime)
	if err != nil {
		return 0, err
	}
	var totalFunding float64
	for i := range incomes {
		val, _ := strconv.ParseFloat(incomes[i].Income, 64)
		totalFunding += val
	}
	return totalFunding, nil
}

// GetFundingRateRaw fetches raw funding rates for debugging.
func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/fundingRate", params, nil)
}

// GetTickersRaw fetches raw ticker logs for debugging.
func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/ticker/24hr", params, nil)
}

// GetOpenPositionsRaw fetches raw positions for debugging.
func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/positionRisk", params, nil)
}

// GetHistoryPositionsRaw fetches raw position history for debugging.
func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/positionRisk", params, nil)
}

// GetOrderDetailRaw fetches raw order detail info for debugging.
func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	p := make(map[string]string)
	maps.Copy(p, params)
	p[paramOrderId] = orderID
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/order", p, nil)
}

// GetHistoryOrdersRaw fetches raw order history logs for debugging.
func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/allOrders", params, nil)
}

// GetOrderPNLRaw fetches raw realized pnl logs for debugging.
func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.RawRequest(ctx, http.MethodGet, "/fapi/v3/income", params, nil)
}

type asterListenKeyResponse struct {
	ListenKey string `json:"listenKey"`
}

func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	body, err := c.request(ctx, http.MethodPost, "/fapi/v3/listenKey", nil, true)
	if err != nil {
		return "", err
	}
	var resp asterListenKeyResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return "", err
	}
	return resp.ListenKey, nil
}

func (c *Client) KeepAliveListenKey(ctx context.Context, listenKey string) error {
	params := map[string]string{
		"listenKey": listenKey,
	}
	_, err := c.request(ctx, http.MethodPut, "/fapi/v3/listenKey", params, true)
	return err
}
