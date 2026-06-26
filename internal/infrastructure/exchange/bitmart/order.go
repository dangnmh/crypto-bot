package bitmart

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

	"github.com/google/uuid"
)

type submitOrderRequest struct {
	Symbol                    string `json:"symbol"`
	ClientOrderID             string `json:"client_order_id,omitempty"`
	Type                      string `json:"type"`
	Side                      int    `json:"side"`
	Leverage                  string `json:"leverage"`
	OpenType                  string `json:"open_type"`
	Mode                      int    `json:"mode"`
	Price                     string `json:"price,omitempty"`
	Size                      int    `json:"size"`
	PresetTakeProfitPriceType int    `json:"preset_take_profit_price_type,omitempty"`
	PresetStopLossPriceType   int    `json:"preset_stop_loss_price_type,omitempty"`
	PresetTakeProfitPrice     string `json:"preset_take_profit_price,omitempty"`
	PresetStopLossPrice       string `json:"preset_stop_loss_price,omitempty"`
}

type bitmartOrderInfo struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id"`
	Symbol        string `json:"symbol"`
	Side          int    `json:"side"`
	Type          string `json:"type"`
	Price         string `json:"price"`
	Size          string `json:"size"`
	DealSize      string `json:"deal_size"`
	FilledValue   string `json:"filled_value"`
	DealAvgPrice  string `json:"deal_avg_price"`
	State         int    `json:"state"`
	CreateTime    int64  `json:"create_time"`
	UpdateTime    int64  `json:"update_time"`
}

// Private raw methods.

func (c *Client) rawCreateOrder(ctx context.Context, body []byte) ([]byte, error) {
	return c.requestFull(ctx, http.MethodPost, "/contract/private/submit-order", nil, body, true)
}

func (c *Client) rawCancelOrder(ctx context.Context, body []byte) ([]byte, error) {
	return c.requestFull(ctx, http.MethodPost, "/contract/private/cancel-order", nil, body, true)
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, body []byte) ([]byte, error) {
	return c.requestFull(ctx, http.MethodPost, "/contract/private/cancel-orders", nil, body, true)
}

func (c *Client) rawGetHistoryOrder(ctx context.Context, query map[string]string) ([]bitmartOrderInfo, error) {
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/order-history", query, nil, true)
	if err != nil {
		return nil, err
	}

	var respDirect struct {
		Code int                `json:"code"`
		Data []bitmartOrderInfo `json:"data"`
	}
	if err := xjson.Unmarshal(body, &respDirect); err == nil && respDirect.Code == 1000 {
		return respDirect.Data, nil
	}
	return nil, fmt.Errorf("invalid response")
}

func (c *Client) rawGetOpenOrders(ctx context.Context, query map[string]string) ([]byte, error) {
	return c.requestFull(ctx, http.MethodGet, "/contract/private/open-orders", query, nil, true)
}

// Public mapper methods.

func mapBitmartState(state int) domain.OrderState {
	switch state {
	case 1:
		return domain.OrderStateUntriggered
	case 2:
		return domain.OrderStateNew
	case 3:
		return domain.OrderStatePartiallyFilled
	case 4:
		return domain.OrderStateFilled
	case 5:
		return domain.OrderStatePartial
	case 6:
		return domain.OrderStateCanceled
	default:
		return domain.OrderStateNew
	}
}

func mapBitmartSide(side int) domain.Side {
	switch side {
	case 1:
		return domain.SideOpenLong
	case 2:
		return domain.SideCloseShort
	case 3:
		return domain.SideCloseLong
	case 4:
		return domain.SideOpenShort
	default:
		return domain.SideUnknown
	}
}

func mapSide(side domain.Side) int {
	switch side {
	case domain.SideOpenLong:
		return sideOpenLong
	case domain.SideCloseShort:
		return sideCloseShort
	case domain.SideCloseLong:
		return sideCloseLong
	case domain.SideOpenShort:
		return sideOpenShort
	case domain.SideUnknown:
		return sideOpenLong
	default:
		return sideOpenLong
	}
}

func mapTypeAndMode(ordType domain.OrderType) (string, int) {
	typeStr := orderTypeLimit
	if ordType == domain.OrderTypeMarket {
		typeStr = orderTypeMarket
	}

	var modeVal int
	switch ordType {
	case domain.OrderTypePostOnly:
		modeVal = modeMakerOnly
	case domain.OrderTypeIOC:
		modeVal = modeIOC
	case domain.OrderTypeFOK:
		modeVal = modeFOK
	case domain.OrderTypeLimit, domain.OrderTypeMarket:
		modeVal = modeGTC
	default:
		modeVal = modeGTC
	}
	return typeStr, modeVal
}

func (c *Client) mapOrderInfo(raw *bitmartOrderInfo) *exchange.OrderInfo {
	price := decmath.ParseFloat(raw.Price)
	avgPrice := decmath.ParseFloat(raw.DealAvgPrice)
	vol := decmath.ParseFloat(raw.Size)
	dealVol := decmath.ParseFloat(raw.DealSize)

	return &exchange.OrderInfo{
		OrderID:      raw.OrderID,
		Symbol:       raw.Symbol,
		Price:        price,
		Vol:          vol,
		DealAvgPrice: avgPrice,
		DealVol:      dealVol,
		State:        mapBitmartState(raw.State),
		ExternalOID:  raw.ClientOrderID,
		Side:         mapBitmartSide(raw.Side),
		PositionMode: domain.PositionModeHedge,
		CreateTime:   raw.CreateTime,
		UpdateTime:   raw.UpdateTime,
	}
}

// CreateOrder submits a new order to the exchange.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	clientOID := req.ExternalOID
	if clientOID == "" {
		clientOID = strings.ReplaceAll(uuid.NewString(), "-", "")
	}
	if len(clientOID) > 32 {
		clientOID = clientOID[:32]
	}

	sideVal := mapSide(req.Side)
	typeStr, modeVal := mapTypeAndMode(req.Type)

	openTypeStr := openTypeIsolated
	if req.OpenType == domain.OpenTypeCross {
		openTypeStr = openTypeCross
	}

	submitReq := submitOrderRequest{
		Symbol:        req.Symbol,
		ClientOrderID: clientOID,
		Type:          typeStr,
		Side:          sideVal,
		Leverage:      strconv.Itoa(req.Leverage),
		OpenType:      openTypeStr,
		Mode:          modeVal,
		Size:          int(req.Vol),
	}

	if req.Type != domain.OrderTypeMarket && req.Price > 0 {
		submitReq.Price = strconv.FormatFloat(req.Price, 'f', -1, 64)
	}

	if req.TakeProfitPrice > 0 {
		submitReq.PresetTakeProfitPrice = strconv.FormatFloat(req.TakeProfitPrice, 'f', -1, 64)
		submitReq.PresetTakeProfitPriceType = 1
	}

	if req.StopLossPrice > 0 {
		submitReq.PresetStopLossPrice = strconv.FormatFloat(req.StopLossPrice, 'f', -1, 64)
		submitReq.PresetStopLossPriceType = 1
	}

	bodyBytes, err := xjson.Marshal(submitReq)
	if err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("marshal request: %w", err)
	}

	body, err := c.rawCreateOrder(ctx, bodyBytes)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	var resp struct {
		Code int `json:"code"`
		Data struct {
			OrderID int64 `json:"order_id"`
		} `json:"data"`
	}

	if err := xjson.Unmarshal(body, &resp); err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("unmarshal response: %w", err)
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0

	return exchange.CreateOrderResult{
		OrderID:       strconv.FormatInt(resp.Data.OrderID, 10),
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

// CancelOrder cancels a single order.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	bodyMap := map[string]any{
		paramSymbol:  symbol,
		paramOrderID: orderID,
	}

	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return err
	}

	_, err = c.rawCancelOrder(ctx, bodyBytes)
	return err
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch CancelOrders not implemented on Bitmart")
}

// CancelAllOpenOrders cancels all open orders for a symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	bodyMap := map[string]any{
		paramSymbol: symbol,
	}

	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return err
	}

	_, err = c.rawCancelAllOpenOrders(ctx, bodyBytes)
	return err
}

func (c *Client) getRawHistoryOrder(ctx context.Context, symbol, orderID, clientOrderID string) (*bitmartOrderInfo, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	if orderID != "" {
		query[paramOrderID] = orderID
	}
	if clientOrderID != "" {
		query["client_order_id"] = clientOrderID
	}

	rawOrders, err := c.rawGetHistoryOrder(ctx, query)
	if err != nil {
		return nil, err
	}

	for i := range rawOrders {
		if orderID != "" && rawOrders[i].OrderID == orderID {
			return &rawOrders[i], nil
		}
		if clientOrderID != "" && rawOrders[i].ClientOrderID == clientOrderID {
			return &rawOrders[i], nil
		}
	}

	return nil, fmt.Errorf("order not found in history: orderID=%s clientOrderID=%s", orderID, clientOrderID)
}

func (c *Client) getRawOrderWithFallback(ctx context.Context, symbol, orderID string) (*bitmartOrderInfo, error) {
	query := map[string]string{
		paramSymbol:  symbol,
		paramOrderID: orderID,
	}
	raw, err := c.rawGetOrder(ctx, query)
	if err != nil {
		if apiErr, ok := exchange.IsAPIError(err); ok && apiErr.Code == 40035 {
			historyRaw, historyErr := c.getRawHistoryOrder(ctx, symbol, orderID, "")
			if historyErr == nil {
				return historyRaw, nil
			}
		}
		return nil, err
	}
	return raw, nil
}

// GetOrder retrieves detailed information about a specific order by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	raw, err := c.getRawOrderWithFallback(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return c.mapOrderInfo(raw), nil
}

// GetOrderByExternalID retrieves detailed information about a specific order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	query := map[string]string{
		paramSymbol:       symbol,
		"client_order_id": externalOrderID,
	}
	raw, err := c.rawGetOrder(ctx, query)
	if err == nil {
		return c.mapOrderInfo(raw), nil
	}

	if apiErr, ok := exchange.IsAPIError(err); ok && apiErr.Code == 40035 {
		historyRaw, historyErr := c.getRawHistoryOrder(ctx, symbol, "", externalOrderID)
		if historyErr == nil {
			return c.mapOrderInfo(historyRaw), nil
		}
	}
	return nil, err
}

// GetOpenOrders retrieves all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	body, err := c.rawGetOpenOrders(ctx, query)
	if err != nil {
		return nil, err
	}

	var respDirect struct {
		Code int                `json:"code"`
		Data []bitmartOrderInfo `json:"data"`
	}
	var respWrapped struct {
		Code int `json:"code"`
		Data struct {
			Orders []bitmartOrderInfo `json:"orders"`
		} `json:"data"`
	}

	var rawOrders []bitmartOrderInfo
	if err := xjson.Unmarshal(body, &respWrapped); err == nil && respWrapped.Code == 1000 && len(respWrapped.Data.Orders) > 0 {
		rawOrders = respWrapped.Data.Orders
	} else if err := xjson.Unmarshal(body, &respDirect); err == nil && respDirect.Code == 1000 {
		rawOrders = respDirect.Data
	} else {
		_ = xjson.Unmarshal(body, &respDirect)
		rawOrders = respDirect.Data
	}

	orders := make([]exchange.OrderInfo, 0, len(rawOrders))
	for i := range rawOrders {
		orders = append(orders, *c.mapOrderInfo(&rawOrders[i]))
	}
	return orders, nil
}
