package kucoin

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type kucoinCreateOrderRequest struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Type      string  `json:"type"`
	Size      float64 `json:"size"`
	Price     float64 `json:"price,omitempty"`
	ClientOid string  `json:"clientOid,omitempty"`
}

type kucoinCreateOrderResponse struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type kucoinCancelOrderRequest struct {
	OrderID string `json:"orderId"`
}

type kucoinCancelOrderResponse struct{}

type kucoinOrderRequest struct {
	OrderID string `json:"orderId"`
}

type kucoinOpenOrdersRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type kucoinChangeLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage string `json:"leverage"`
}

type kucoinChangeLeverageResponse struct{}

type kucoinOrder struct {
	OrderID     string `json:"orderId"`
	ClientOid   string `json:"clientOid"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	Type        string `json:"type"`
	Size        int64  `json:"size"`
	Price       string `json:"price"`
	Status      string `json:"status"`
	DealSize    int64  `json:"dealSize"`
	StatusVal   string `json:"statusVal"`
	CreatedAt   int64  `json:"createdAt"`
	FilledValue string `json:"filledValue"`
	IsActive    bool   `json:"isActive"`
}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) createRawOrder(ctx context.Context, req kucoinCreateOrderRequest) (*kucoinCreateOrderResponse, error) {
	bodyMap := map[string]any{
		paramSymbol: req.Symbol,
		"side":      req.Side,
		paramType:   req.Type,
		"size":      req.Size,
	}
	if req.Price > 0 {
		bodyMap["price"] = req.Price
	}
	if req.ClientOid != "" {
		bodyMap["clientOid"] = req.ClientOid
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[kucoinCreateOrderResponse](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, req kucoinCancelOrderRequest) (*kucoinCancelOrderResponse, error) {
	path := fmt.Sprintf("%s/%s", pathCancelOrder, req.OrderID)
	url := c.baseURL + path
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("create DELETE request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiSecret, ts, http.MethodDelete, path, "")
		httpReq.Header.Set(headerKey, c.apiKey)
		httpReq.Header.Set(headerSign, sig)
		httpReq.Header.Set(headerTimestamp, ts)
		httpReq.Header.Set(headerAuthPhrase, SignPassphrase(c.apiSecret, c.passphrase))
		httpReq.Header.Set(headerVersion, "2")
	}

	body, err := c.doRequest(ctx, httpReq)
	if err != nil {
		return nil, err
	}

	if err := ParseResponseIgnoreData(body, "cancel_order"); err != nil {
		return nil, err
	}
	return &kucoinCancelOrderResponse{}, nil
}

func (c *Client) getRawOrder(ctx context.Context, req kucoinOrderRequest) (*kucoinOrder, error) {
	path := fmt.Sprintf("%s/%s", pathGetOrder, req.OrderID)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[kucoinOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, req kucoinOpenOrdersRequest) ([]kucoinOrder, error) {
	params := map[string]string{
		"status": "active",
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	type orderListData struct {
		Items []kucoinOrder `json:"items"`
	}

	var rawList []kucoinOrder
	listParsed, err := ParseResponse[orderListData](body, "open_orders")
	if err == nil {
		rawList = listParsed.Items
	} else {
		directParsed, err := ParseResponse[[]kucoinOrder](body, "open_orders")
		if err == nil {
			rawList = directParsed
		} else {
			return nil, fmt.Errorf("parse open orders failed: %w", err)
		}
	}

	return rawList, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, req kucoinChangeLeverageRequest) (*kucoinChangeLeverageResponse, error) {
	bodyMap := map[string]any{
		paramSymbol: req.Symbol,
		"leverage":  req.Leverage,
	}

	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return nil, err
	}

	if err := ParseResponseIgnoreData(body, "set_leverage"); err != nil {
		return nil, err
	}
	return &kucoinChangeLeverageResponse{}, nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType := "limit"
	if req.Type == exchange.OrderTypeMarket {
		ordType = "market"
	}

	side := sideBuy
	if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseLong {
		side = sideSell
	}

	rawReq := kucoinCreateOrderRequest{
		Symbol:    req.Symbol,
		Side:      side,
		Type:      ordType,
		Size:      req.Vol,
		ClientOid: req.ExternalOID,
	}

	if req.Type != exchange.OrderTypeMarket {
		rawReq.Price = req.Price
	}

	res, err := c.createRawOrder(ctx, rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	return exchange.CreateOrderResult{
		OrderID:       res.OrderID,
		TPSLSubmitted: false,
	}, nil
}

// CreateTrackOrder is a placeholder.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented on KuCoin")
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	_, err := c.cancelRawOrder(ctx, kucoinCancelOrderRequest{
		OrderID: orderID,
	})
	return err
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch CancelOrders not implemented on KuCoin")
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	orders, err := c.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range orders {
		_ = c.CancelOrder(ctx, symbol, orders[i].OrderID)
	}

	return nil
}

// GetOrder fetches details of a specific order.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrder(ctx, kucoinOrderRequest{
		OrderID: orderID,
	})
	if err != nil {
		return nil, err
	}

	return c.toOrderInfo(raw), nil
}

// GetOpenOrders returns all currently active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.getRawOpenOrders(ctx, kucoinOpenOrdersRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	infos := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		infos = append(infos, *c.toOrderInfo(&rawList[i]))
	}

	return infos, nil
}

// ClosePosition is a helper to close a position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	submitSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		submitSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Side:         submitSide,
		Type:         exchange.OrderTypeMarket,
		Vol:          volume,
		PositionMode: positionMode,
	})
	return err
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := &positions[i]
		closeSide := domain.SideCloseLong
		if pos.PositionType == 2 { // 2 = Short
			closeSide = domain.SideCloseShort
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1)
	}

	return nil
}

// ChangeLeverage changes leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	_, err := c.changeRawLeverage(ctx, kucoinChangeLeverageRequest{
		Symbol:   req.Symbol,
		Leverage: strconv.Itoa(req.Leverage),
	})
	return err
}

func (c *Client) toOrderInfo(o *kucoinOrder) *exchange.OrderInfo {
	state := 0 // default active/pending
	if !o.IsActive {
		if o.Status == stateFilled || o.StatusVal == "done" {
			state = exchange.OrderStateFilled
		} else {
			state = exchange.OrderStateCanceled
		}
	}

	sideVal := exchange.SideOpenLong
	if o.Side == sideSell {
		sideVal = exchange.SideOpenShort
	}

	price := decmath.ParseFloat(o.Price)
	qty := float64(o.Size)
	exec := float64(o.DealSize)

	var avg float64
	val := decmath.ParseFloat(o.FilledValue)
	if exec > 0 {
		avg = val / exec
	}

	return &exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        price,
		Vol:          qty,
		DealVol:      exec,
		DealAvgPrice: avg,
		State:        state,
		Side:         sideVal,
	}
}
