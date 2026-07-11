package pionex

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

// RawRequest executes a signed private API request and returns raw bytes.
func (c *Client) RawRequest(ctx context.Context, method, path string, query map[string]string, body []byte) ([]byte, error) {
	return c.rawRequestPrivate(ctx, method, path, query, body)
}

func (c *Client) rawRequestPrivate(ctx context.Context, method, path string, params map[string]string, body []byte) ([]byte, error) {
	if c.limiter != nil {
		if err := c.limiter.Acquire(ctx, path); err != nil {
			return nil, fmt.Errorf("rate limit acquire: %w", err)
		}
	}

	if params == nil {
		params = make(map[string]string)
	}
	params["timestamp"] = strconv.FormatInt(c.clock.Now().UnixMilli(), 10)

	// Build sorted query string without URL encoding for signature
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, k+"="+params[k])
	}
	queryString := strings.Join(parts, "&")

	// Construct PATH_URL for signature
	pathURL := path + "?" + queryString

	// Build message base
	message := strings.ToUpper(method) + pathURL
	if len(body) > 0 {
		message += string(body)
	}

	// Calculate HMAC SHA256 hex signature
	h := hmac.New(sha256.New, []byte(c.apiSecret))
	h.Write([]byte(message))
	signature := hex.EncodeToString(h.Sum(nil))

	keyMask := "empty"
	if len(c.apiKey) > 4 {
		keyMask = c.apiKey[:4] + "..."
	}
	c.logger.Info("Pionex Request Details",
		"method", method,
		"pathURL", pathURL,
		"messageToSign", message,
		"apiKey", keyMask,
		"signature", signature,
	)

	// Construct request URL
	reqURL, err := url.Parse(c.baseURL + pathURL)
	if err != nil {
		return nil, err
	}

	// Create request
	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, reqURL.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	// Add headers
	req.Header.Set("PIONEX-KEY", c.apiKey)
	req.Header.Set("PIONEX-SIGNATURE", signature)
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	// Do request
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("http error %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// RawRequest Interface Implementations.

func (c *Client) GetFundingRateRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPublic(ctx, "GET", "/api/v1/market/indexes", params)
}

func (c *Client) GetTickersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPublic(ctx, "GET", "/api/v1/market/tickers", params)
}

func (c *Client) GetOpenPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/account/positions", params, nil)
}

func (c *Client) GetHistoryPositionsRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/account/historyPositions", params, nil)
}

func (c *Client) GetOrderDetailRaw(ctx context.Context, orderID string, params map[string]string) ([]byte, error) {
	if params == nil {
		params = make(map[string]string)
	}
	params["orderId"] = orderID
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/order", params, nil)
}

func (c *Client) GetHistoryOrdersRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/historyOrders", params, nil)
}

func (c *Client) GetOrderPNLRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/fillsByOrderId", params, nil)
}

// GetFundingFeeRaw queries funding fee payment records.
func (c *Client) GetFundingFeeRaw(ctx context.Context, params map[string]string) ([]byte, error) {
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/fundingFee", params, nil)
}

// OrderExecutor Interface Stub Implementations.

type pionexSubmitOrderRequest struct {
	ClientOrderID string `json:"clientOrderId"`
	Symbol        string `json:"symbol"`
	PositionSide  string `json:"positionSide"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Size          string `json:"size"`
	Price         string `json:"price,omitempty"`
	ReduceOnly    bool   `json:"reduceOnly"`
}

type pionexSubmitOrderResponse struct {
	Result    bool         `json:"result"`
	Timestamp xjson.Number `json:"timestamp"`
	Data      struct {
		OrderID xjson.Number `json:"orderId"`
	} `json:"data"`
}

func (c *Client) rawCreateOrder(ctx context.Context, req pionexSubmitOrderRequest) ([]byte, error) {
	body, err := xjson.Marshal(req)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "POST", "/uapi/v1/trade/order", nil, body)
}

func mapOrderSide(positionMode domain.PositionMode, side domain.Side) (string, string, bool, error) {
	if positionMode == domain.PositionModeHedge {
		switch side {
		case domain.SideOpenLong:
			return string(OrderSideBuy), string(PositionSideLong), false, nil
		case domain.SideCloseLong:
			return string(OrderSideSell), string(PositionSideLong), false, nil
		case domain.SideOpenShort:
			return string(OrderSideSell), string(PositionSideShort), false, nil
		case domain.SideCloseShort:
			return string(OrderSideBuy), string(PositionSideShort), false, nil
		default:
			return "", "", false, fmt.Errorf("unsupported order side for hedge mode: %v", side)
		}
	}

	switch side {
	case domain.SideOpenLong:
		return string(OrderSideBuy), bothSide, false, nil
	case domain.SideCloseShort:
		return string(OrderSideBuy), bothSide, true, nil
	case domain.SideOpenShort:
		return string(OrderSideSell), bothSide, false, nil
	case domain.SideCloseLong:
		return string(OrderSideSell), bothSide, true, nil
	default:
		return "", "", false, fmt.Errorf("unsupported order side for one-way mode: %v", side)
	}
}

func mapOrderType(orderType domain.OrderType) (string, error) {
	switch orderType {
	case domain.OrderTypeLimit:
		return "LIMIT", nil
	case domain.OrderTypeMarket:
		return "MARKET_QTY", nil
	case domain.OrderTypePostOnly:
		return "POSTONLY", nil
	case domain.OrderTypeIOC:
		return "IOC", nil
	case domain.OrderTypeFOK:
		return "FOK", nil
	default:
		return "", fmt.Errorf("unsupported order type: %v", orderType)
	}
}

func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	sideStr, posSideStr, reduceOnly, err := mapOrderSide(req.PositionMode, req.Side)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	typeStr, err := mapOrderType(req.Type)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	pionexReq := pionexSubmitOrderRequest{
		ClientOrderID: req.ExternalOID,
		Symbol:        req.Symbol,
		PositionSide:  posSideStr,
		Side:          sideStr,
		Type:          typeStr,
		Size:          strconv.FormatFloat(req.Vol, 'f', -1, 64),
		ReduceOnly:    reduceOnly,
	}

	if req.Type != domain.OrderTypeMarket && req.Price > 0 {
		pionexReq.Price = strconv.FormatFloat(req.Price, 'f', -1, 64)
	}

	body, err := c.rawCreateOrder(ctx, pionexReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	var resp pionexSubmitOrderResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("unmarshal pionex create order: %w", err)
	}
	if !resp.Result {
		return exchange.CreateOrderResult{}, fmt.Errorf("pionex create order failed")
	}

	return exchange.CreateOrderResult{
		OrderID: strconv.FormatInt(xjson.ToInt64(resp.Data.OrderID), 10),
	}, nil
}

type pionexCancelOrderRequest struct {
	Symbol  string `json:"symbol"`
	OrderID int64  `json:"orderId"`
}

func (c *Client) rawCancelOrder(ctx context.Context, symbol string, orderID int64) ([]byte, error) {
	reqBody := pionexCancelOrderRequest{
		Symbol:  symbol,
		OrderID: orderID,
	}
	body, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "DELETE", "/uapi/v1/trade/order", nil, body)
}

func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	orderIDInt, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid orderID: %w", err)
	}
	body, err := c.rawCancelOrder(ctx, symbol, orderIDInt)
	if err != nil {
		return err
	}
	var resp pionexBaseResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unmarshal pionex cancel order: %w", err)
	}
	if !resp.Result {
		return fmt.Errorf("pionex cancel order failed")
	}
	return nil
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		if err := c.CancelOrder(ctx, "", id); err != nil {
			return err
		}
	}
	return nil
}

type pionexCancelAllOpenOrdersRequest struct {
	Symbol string `json:"symbol"`
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, symbol string) ([]byte, error) {
	reqBody := pionexCancelAllOpenOrdersRequest{
		Symbol: symbol,
	}
	body, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "DELETE", "/uapi/v1/trade/allOrders", nil, body)
}

func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	body, err := c.rawCancelAllOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}
	var resp pionexBaseResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unmarshal pionex cancel all open orders: %w", err)
	}
	if !resp.Result {
		return fmt.Errorf("pionex cancel all open orders failed")
	}
	return nil
}

type pionexOrderResponse struct {
	Result    bool         `json:"result"`
	Timestamp xjson.Number `json:"timestamp"`
	Data      pionexOrder  `json:"data"`
}

type pionexOrder struct {
	OrderID       xjson.Number `json:"orderId"`
	Symbol        string       `json:"symbol"`
	Type          string       `json:"type"`
	PositionMode  string       `json:"positionMode"`
	IsolatedMode  string       `json:"isolatedMode"`
	Side          string       `json:"side"`
	PositionSide  string       `json:"positionSide"`
	Price         xjson.Number `json:"price"`
	OrigSize      xjson.Number `json:"origSize"`
	Size          xjson.Number `json:"size"`
	FilledSize    xjson.Number `json:"filledSize"`
	FilledAmount  xjson.Number `json:"filledAmount"`
	Status        string       `json:"status"`
	ClientOrderID string       `json:"clientOrderId"`
	ReduceOnly    bool         `json:"reduceOnly"`
	CreateTime    xjson.Number `json:"createTime"`
	UpdateTime    xjson.Number `json:"updateTime"`
}

func mapOrderState(status string, filledSizeVal float64) domain.OrderState {
	switch status {
	case "OPEN":
		if filledSizeVal > 0 {
			return domain.OrderStatePartiallyFilled
		}
		return domain.OrderStateNew
	case "FILLED":
		return domain.OrderStateFilled
	case "CANCELED":
		if filledSizeVal > 0 {
			return domain.OrderStatePartial
		}
		return domain.OrderStateCanceled
	default:
		return domain.OrderStateNew
	}
}

func mapHedgeOrderSide(rawSide, rawPosSide string) domain.Side {
	switch {
	case rawSide == string(OrderSideBuy) && rawPosSide == string(PositionSideLong):
		return domain.SideOpenLong
	case rawSide == string(OrderSideSell) && rawPosSide == string(PositionSideLong):
		return domain.SideCloseLong
	case rawSide == string(OrderSideSell) && rawPosSide == string(PositionSideShort):
		return domain.SideOpenShort
	case rawSide == string(OrderSideBuy) && rawPosSide == string(PositionSideShort):
		return domain.SideCloseShort
	default:
		return domain.SideUnknown
	}
}

func mapOneWayOrderSide(rawSide string, reduceOnly bool) domain.Side {
	switch {
	case rawSide == string(OrderSideBuy) && !reduceOnly:
		return domain.SideOpenLong
	case rawSide == string(OrderSideBuy) && reduceOnly:
		return domain.SideCloseShort
	case rawSide == string(OrderSideSell) && !reduceOnly:
		return domain.SideOpenShort
	case rawSide == string(OrderSideSell) && reduceOnly:
		return domain.SideCloseLong
	default:
		return domain.SideUnknown
	}
}

func mapOrderSideAndMode(rawSide, rawPosSide, rawPosMode string, reduceOnly bool) (domain.PositionMode, domain.Side) {
	var posMode domain.PositionMode
	if rawPosMode == openCloseMode {
		posMode = domain.PositionModeHedge
	} else {
		posMode = domain.PositionModeOneWay
	}

	var side domain.Side
	if posMode == domain.PositionModeHedge {
		side = mapHedgeOrderSide(rawSide, rawPosSide)
	} else {
		side = mapOneWayOrderSide(rawSide, reduceOnly)
	}
	return posMode, side
}

type commonOrderFields struct {
	OrderID       xjson.Number
	Symbol        string
	Side          string
	PositionSide  string
	PositionMode  string
	Price         xjson.Number
	OrigSize      xjson.Number
	Size          xjson.Number
	FilledSize    xjson.Number
	FilledAmount  xjson.Number
	Status        string
	ClientOrderID string
	ReduceOnly    bool
	CreateTime    xjson.Number
	UpdateTime    xjson.Number
}

func mapCommonOrderInfo(raw commonOrderFields) (exchange.OrderInfo, error) {
	priceVal := xjson.ToFloat64(raw.Price)
	origSizeVal := xjson.ToFloat64(raw.OrigSize)
	if origSizeVal == 0 {
		origSizeVal = xjson.ToFloat64(raw.Size)
	}
	filledSizeVal := xjson.ToFloat64(raw.FilledSize)
	var dealAvgPrice float64
	if filledSizeVal > 0 {
		filledAmtVal := xjson.ToFloat64(raw.FilledAmount)
		dealAvgPrice = filledAmtVal / filledSizeVal
	}

	state := mapOrderState(raw.Status, filledSizeVal)
	posMode, side := mapOrderSideAndMode(raw.Side, raw.PositionSide, raw.PositionMode, raw.ReduceOnly)

	return exchange.OrderInfo{
		OrderID:      strconv.FormatInt(xjson.ToInt64(raw.OrderID), 10),
		Symbol:       raw.Symbol,
		Price:        priceVal,
		Vol:          origSizeVal,
		DealAvgPrice: dealAvgPrice,
		DealVol:      filledSizeVal,
		State:        state,
		ExternalOID:  raw.ClientOrderID,
		Side:         side,
		PositionMode: posMode,
		CreateTime:   xjson.ToInt64(raw.CreateTime),
		UpdateTime:   xjson.ToInt64(raw.UpdateTime),
	}, nil
}

func mapOrderInfo(raw pionexOrder) (exchange.OrderInfo, error) {
	return mapCommonOrderInfo(commonOrderFields{
		OrderID:       raw.OrderID,
		Symbol:        raw.Symbol,
		Side:          raw.Side,
		PositionSide:  raw.PositionSide,
		PositionMode:  raw.PositionMode,
		Price:         raw.Price,
		OrigSize:      raw.OrigSize,
		Size:          raw.Size,
		FilledSize:    raw.FilledSize,
		FilledAmount:  raw.FilledAmount,
		Status:        raw.Status,
		ClientOrderID: raw.ClientOrderID,
		ReduceOnly:    raw.ReduceOnly,
		CreateTime:    raw.CreateTime,
		UpdateTime:    raw.UpdateTime,
	})
}

func (c *Client) rawGetOrder(ctx context.Context, symbol, orderID string) ([]byte, error) {
	params := map[string]string{
		symbolKey: symbol,
		"orderId": orderID,
	}
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/order", params, nil)
}

func (c *Client) rawGetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) ([]byte, error) {
	params := map[string]string{
		symbolKey:       symbol,
		"clientOrderId": clientOrderID,
	}
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/orderByClientOrderId", params, nil)
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	body, err := c.rawGetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	var resp pionexOrderResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex order: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex get order failed")
	}
	info, err := mapOrderInfo(resp.Data)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	body, err := c.rawGetOrderByClientOrderID(ctx, symbol, externalOrderID)
	if err != nil {
		return nil, err
	}
	var resp pionexOrderResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex order: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex get order by client order ID failed")
	}
	info, err := mapOrderInfo(resp.Data)
	if err != nil {
		return nil, err
	}
	return &info, nil
}

type pionexOpenOrder struct {
	OrderID       xjson.Number `json:"orderId"`
	Symbol        string       `json:"symbol"`
	Type          string       `json:"type"`
	PositionMode  string       `json:"positionMode"`
	IsolatedMode  string       `json:"isolatedMode"`
	Side          string       `json:"side"`
	PositionSide  string       `json:"positionSide"`
	Price         xjson.Number `json:"price"`
	OrigSize      xjson.Number `json:"origSize"`
	Size          xjson.Number `json:"size"`
	FilledSize    xjson.Number `json:"filledSize"`
	FilledAmount  xjson.Number `json:"filledAmount"`
	Status        string       `json:"status"`
	ClientOrderID string       `json:"clientOrderId"`
	ReduceOnly    bool         `json:"reduceOnly"`
	CreateTime    xjson.Number `json:"createTime"`
	UpdateTime    xjson.Number `json:"updateTime"`
}

type pionexOpenOrdersData struct {
	Orders []pionexOpenOrder `json:"orders"`
}

type pionexOpenOrdersResponse struct {
	Result    bool                 `json:"result"`
	Timestamp xjson.Number         `json:"timestamp"`
	Data      pionexOpenOrdersData `json:"data"`
}

func mapOpenOrderInfo(raw pionexOpenOrder) (exchange.OrderInfo, error) {
	return mapCommonOrderInfo(commonOrderFields{
		OrderID:       raw.OrderID,
		Symbol:        raw.Symbol,
		Side:          raw.Side,
		PositionSide:  raw.PositionSide,
		PositionMode:  raw.PositionMode,
		Price:         raw.Price,
		OrigSize:      raw.OrigSize,
		Size:          raw.Size,
		FilledSize:    raw.FilledSize,
		FilledAmount:  raw.FilledAmount,
		Status:        raw.Status,
		ClientOrderID: raw.ClientOrderID,
		ReduceOnly:    raw.ReduceOnly,
		CreateTime:    raw.CreateTime,
		UpdateTime:    raw.UpdateTime,
	})
}

func (c *Client) rawGetOpenOrders(ctx context.Context, symbol string) ([]byte, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[symbolKey] = symbol
	}
	return c.rawRequestPrivate(ctx, "GET", "/uapi/v1/trade/openOrders", params, nil)
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	body, err := c.rawGetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	var resp pionexOpenOrdersResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex open orders: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex get open orders failed")
	}

	out := make([]exchange.OrderInfo, 0, len(resp.Data.Orders))
	for i := range resp.Data.Orders {
		info, err := mapOpenOrderInfo(resp.Data.Orders[i])
		if err != nil {
			return nil, err
		}
		out = append(out, info)
	}
	return out, nil
}

type pionexPosition struct {
	PositionID       string       `json:"positionId"`
	Symbol           string       `json:"symbol"`
	IsolatedMode     string       `json:"isolatedMode"`
	RiskState        string       `json:"riskState"`
	PositionSide     string       `json:"positionSide"`
	NetSize          xjson.Number `json:"netSize"`
	AvgPrice         xjson.Number `json:"avgPrice"`
	UnrealizedPnL    xjson.Number `json:"unrealizedPnL"`
	SizeLong         xjson.Number `json:"sizeLong"`
	SizeShort        xjson.Number `json:"sizeShort"`
	AmountLong       xjson.Number `json:"amountLong"`
	AmountShort      xjson.Number `json:"amountShort"`
	AmountSettled    xjson.Number `json:"amountSettled"`
	MarkPrice        xjson.Number `json:"markPrice"`
	InitialMargin    xjson.Number `json:"initialMargin"`
	MaintMargin      xjson.Number `json:"maintMargin"`
	LiquidationPrice xjson.Number `json:"liquidationPrice"`
	Leverage         xjson.Number `json:"leverage"`
	SizeLiquidated   xjson.Number `json:"sizeLiquidated"`
	AmountLiquidated xjson.Number `json:"amountLiquidated"`
	CreateTime       xjson.Number `json:"createTime"`
	UpdateTime       xjson.Number `json:"updateTime"`
}

type pionexPositionsData struct {
	Positions []pionexPosition `json:"positions"`
}

type pionexPositionsResponse struct {
	Result    bool                `json:"result"`
	Timestamp xjson.Number        `json:"timestamp"`
	Data      pionexPositionsData `json:"data"`
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func mapPosition(raw pionexPosition) exchange.Position {
	netSizeVal := xjson.ToFloat64(raw.NetSize)
	var posType exchange.PositionType
	switch raw.PositionSide {
	case "LONG":
		posType = exchange.PositionTypeLong
	case "SHORT":
		posType = exchange.PositionTypeShort
	default:
		switch {
		case netSizeVal > 0:
			posType = exchange.PositionTypeLong
		case netSizeVal < 0:
			posType = exchange.PositionTypeShort
		default:
			posType = exchange.PositionTypeUnknown
		}
	}

	return exchange.Position{
		Symbol:       raw.Symbol,
		HoldVol:      absFloat(netSizeVal),
		PositionType: posType,
		OpenAvgPrice: xjson.ToFloat64(raw.AvgPrice),
		HoldAvgPrice: xjson.ToFloat64(raw.AvgPrice),
		Leverage:     int(xjson.ToInt64(raw.Leverage)),
	}
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[symbolKey] = symbol
	}
	body, err := c.GetOpenPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	var resp pionexPositionsResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pionex positions: %w", err)
	}
	if !resp.Result {
		return nil, fmt.Errorf("pionex get open positions failed")
	}

	out := make([]exchange.Position, 0, len(resp.Data.Positions))
	for i := range resp.Data.Positions {
		out = append(out, mapPosition(resp.Data.Positions[i]))
	}
	return out, nil
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "pionex"),
		Leverage:     leverage,
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

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
			err = c.ClosePosition(ctx, pos.Symbol, side, pos.HoldVol, domain.PositionModeHedge, pos.Leverage)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type pionexHistoryOrder struct {
	OrderID       xjson.Number `json:"orderId"`
	Symbol        string       `json:"symbol"`
	Type          string       `json:"type"`
	PositionMode  string       `json:"positionMode"`
	IsolatedMode  string       `json:"isolatedMode"`
	Side          string       `json:"side"`
	PositionSide  string       `json:"positionSide"`
	Price         xjson.Number `json:"price"`
	OrigSize      xjson.Number `json:"origSize"`
	Size          xjson.Number `json:"size"`
	FilledSize    xjson.Number `json:"filledSize"`
	FilledAmount  xjson.Number `json:"filledAmount"`
	Status        string       `json:"status"`
	ClientOrderID string       `json:"clientOrderId"`
	ReduceOnly    bool         `json:"reduceOnly"`
	CreateTime    xjson.Number `json:"createTime"`
	UpdateTime    xjson.Number `json:"updateTime"`
}

type pionexHistoryOrdersData struct {
	Orders []pionexHistoryOrder `json:"orders"`
}

type pionexHistoryOrdersResponse struct {
	Result    bool                    `json:"result"`
	Timestamp xjson.Number            `json:"timestamp"`
	Data      pionexHistoryOrdersData `json:"data"`
}

type pionexHistoryPosition struct {
	PositionID       string       `json:"positionId"`
	Symbol           string       `json:"symbol"`
	IsolatedMode     string       `json:"isolatedMode"`
	PositionSide     string       `json:"positionSide"`
	SizeLong         xjson.Number `json:"sizeLong"`
	SizeShort        xjson.Number `json:"sizeShort"`
	AmountLong       xjson.Number `json:"amountLong"`
	AmountShort      xjson.Number `json:"amountShort"`
	AmountSettled    xjson.Number `json:"amountSettled"`
	Leverage         xjson.Number `json:"leverage"`
	SizeLiquidated   xjson.Number `json:"sizeLiquidated"`
	AmountLiquidated xjson.Number `json:"amountLiquidated"`
	SizeTakeover     xjson.Number `json:"sizeTakeover"`
	CreateTime       xjson.Number `json:"createTime"`
	UpdateTime       xjson.Number `json:"updateTime"`
}

type pionexHistoryPositionsData struct {
	Positions []pionexHistoryPosition `json:"positions"`
}

type pionexHistoryPositionsResponse struct {
	Result    bool                       `json:"result"`
	Timestamp xjson.Number               `json:"timestamp"`
	Data      pionexHistoryPositionsData `json:"data"`
}

var (
	_ = pionexHistoryOrder{}
	_ = pionexHistoryOrdersData{}
	_ = pionexHistoryOrdersResponse{}
	_ = pionexPosition{}
	_ = pionexPositionsData{}
	_ = pionexPositionsResponse{}
	_ = pionexHistoryPosition{}
	_ = pionexHistoryPositionsData{}
	_ = pionexHistoryPositionsResponse{}
)
