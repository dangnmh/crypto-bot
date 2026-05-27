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

type kucoinOrderResult struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

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

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	ordType := "limit"
	if req.Type == exchange.OrderTypeMarket {
		ordType = "market"
	}

	side := sideBuy
	if req.Side == exchange.SideOpenShort || req.Side == exchange.SideCloseLong {
		side = sideSell
	}

	bodyMap := map[string]interface{}{
		paramSymbol: req.Symbol,
		"side":      side,
		paramType:   ordType,
		"size":      req.Vol,
	}

	if req.Type != exchange.OrderTypeMarket {
		bodyMap["price"] = req.Price
	}

	if req.ExternalOID != "" {
		bodyMap["clientOid"] = req.ExternalOID
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return "", err
	}

	res, err := ParseResponse[kucoinOrderResult](body, "create_order")
	if err != nil {
		return "", err
	}

	return res.OrderID, nil
}

// CreateTrackOrder is a placeholder.
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented on KuCoin")
}

// CancelOrder cancels an existing order by ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	path := fmt.Sprintf("%s/%s", pathCancelOrder, orderID)
	// KuCoin cancel request is DELETE
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, http.NoBody)
	if err != nil {
		return fmt.Errorf("create DELETE request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		sig := SignRequest(c.apiSecret, ts, http.MethodDelete, path, "")
		req.Header.Set(headerKey, c.apiKey)
		req.Header.Set(headerSign, sig)
		req.Header.Set(headerTimestamp, ts)
		req.Header.Set(headerAuthPhrase, SignPassphrase(c.apiSecret, c.passphrase))
		req.Header.Set(headerVersion, "2")
	}

	body, err := c.doRequest(ctx, req)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "cancel_order")
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
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	path := fmt.Sprintf("%s/%s", pathGetOrder, orderID)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[kucoinOrder](body, "get_order")
	if err != nil {
		return nil, err
	}

	return c.toOrderInfo(&res), nil
}

// GetOpenOrders returns all currently active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{
		"status": "active",
	}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	// KuCoin Pending Orders can return a paginated object or list
	type orderListData struct {
		Items []kucoinOrder `json:"items"`
	}

	var rawList []kucoinOrder
	listParsed, err := ParseResponse[orderListData](body, "open_orders")
	if err == nil {
		rawList = listParsed.Items
	} else {
		// Try parsing directly as list
		directParsed, err := ParseResponse[[]kucoinOrder](body, "open_orders")
		if err == nil {
			rawList = directParsed
		} else {
			return nil, fmt.Errorf("parse open orders failed: %w", err)
		}
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
	bodyMap := map[string]interface{}{
		paramSymbol: req.Symbol,
		"leverage":  strconv.Itoa(req.Leverage),
	}

	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "set_leverage")
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
