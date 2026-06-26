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

type bitmartPosition struct {
	Symbol         string `json:"symbol"`
	PositionAmt    string `json:"position_amt"`
	PositionAmount string `json:"position_amount"`
	AvgEntryPrice  string `json:"avg_entry_price"`
	OpenAvgPrice   string `json:"open_avg_price"`
	UnrealizedPnL  string `json:"unrealized_pnl"`
	Leverage       string `json:"leverage"`
	OpenType       string `json:"open_type"`
	PositionSide   string `json:"position_side"`
}

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

	body, err := c.requestFull(ctx, http.MethodPost, "/contract/private/submit-order", nil, bodyBytes, true)
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

	_, err = c.requestFull(ctx, http.MethodPost, "/contract/private/cancel-order", nil, bodyBytes, true)
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

	_, err = c.requestFull(ctx, http.MethodPost, "/contract/private/cancel-orders", nil, bodyBytes, true)
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

	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/order-history", query, nil, true)
	if err != nil {
		return nil, err
	}

	var respDirect struct {
		Code int                `json:"code"`
		Data []bitmartOrderInfo `json:"data"`
	}
	var rawOrders []bitmartOrderInfo
	if err := xjson.Unmarshal(body, &respDirect); err == nil && respDirect.Code == 1000 {
		rawOrders = respDirect.Data
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
	raw, err := c.getRawOrder(ctx, symbol, orderID)
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
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/order", query, nil, true)
	if err != nil {
		if apiErr, ok := exchange.IsAPIError(err); ok && apiErr.Code == 40035 {
			historyRaw, historyErr := c.getRawHistoryOrder(ctx, symbol, "", externalOrderID)
			if historyErr == nil {
				return c.mapOrderInfo(historyRaw), nil
			}
		}
		return nil, err
	}
	var resp struct {
		Code int              `json:"code"`
		Data bitmartOrderInfo `json:"data"`
	}
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return c.mapOrderInfo(&resp.Data), nil
}

// GetOpenOrders retrieves all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/open-orders", query, nil, true)
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

// GetOpenPositions returns all open futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	query := make(map[string]string)
	if symbol != "" {
		query[paramSymbol] = symbol
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/position-v2", query, nil, true)
	if err != nil {
		return nil, err
	}

	rawPositions, err := unmarshalPositions(body)
	if err != nil {
		return nil, err
	}

	var positions []exchange.Position
	for i := range rawPositions {
		raw := &rawPositions[i]
		if symbol != "" && raw.Symbol != symbol {
			continue
		}
		p := mapPosition(raw)
		if p != nil {
			positions = append(positions, *p)
		}
	}
	return positions, nil
}

func unmarshalPositions(body []byte) ([]bitmartPosition, error) {
	var respDirect struct {
		Code int               `json:"code"`
		Data []bitmartPosition `json:"data"`
	}

	if err := xjson.Unmarshal(body, &respDirect); err == nil && respDirect.Code == 1000 {
		return respDirect.Data, nil
	}
	return nil, fmt.Errorf("invalid response")
}

func mapPosition(raw *bitmartPosition) *exchange.Position {
	vol := decmath.ParseFloat(raw.PositionAmt)
	if vol == 0 {
		vol = decmath.ParseFloat(raw.PositionAmount)
	}
	if vol == 0 {
		return nil
	}

	pType := exchange.PositionTypeLong
	if strings.EqualFold(raw.PositionSide, posSideShort) || raw.PositionSide == "2" {
		pType = exchange.PositionTypeShort
	}

	avgPrice := decmath.ParseFloat(raw.AvgEntryPrice)
	if avgPrice == 0 {
		avgPrice = decmath.ParseFloat(raw.OpenAvgPrice)
	}
	pnl := decmath.ParseFloat(raw.UnrealizedPnL)

	levVal, _ := strconv.Atoi(raw.Leverage)
	return &exchange.Position{
		Symbol:          raw.Symbol,
		HoldVol:         vol,
		PositionType:    pType,
		OpenAvgPrice:    avgPrice,
		HoldAvgPrice:    avgPrice,
		CloseProfitLoss: pnl,
		Leverage:        levVal,
	}
}

// ClosePosition closes a position by submitting a market reduction order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	submitSide := domain.SideCloseLong
	if closeSide == domain.SideCloseShort {
		submitSide = domain.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Side:         submitSide,
		Type:         domain.OrderTypeMarket,
		Vol:          volume,
		PositionMode: positionMode,
		ExternalOID:  uuid.NewString(),
		Leverage:     leverage,
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
		if pos.PositionType == exchange.PositionTypeShort {
			closeSide = domain.SideCloseShort
		}
		err = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, domain.PositionModeHedge, pos.Leverage)
		if err != nil {
			return err
		}
	}

	return nil
}

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	openTypeStr := openTypeIsolated
	if req.OpenType == domain.OpenTypeCross {
		openTypeStr = openTypeCross
	}
	bodyMap := map[string]any{
		paramSymbol: req.Symbol,
		"leverage":  strconv.Itoa(req.Leverage),
		"open_type": openTypeStr,
	}
	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return err
	}
	_, err = c.requestFull(ctx, http.MethodPost, "/contract/private/submit-leverage", nil, bodyBytes, true)
	return err
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED). Bitmart does it per order so this is a no-op.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}

// SwitchPositionMode switches hold mode between hedge and one-way.
func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	modeStr := "hedge_mode"
	if positionMode == domain.PositionModeOneWay {
		modeStr = "one_way_mode"
	}
	bodyMap := map[string]any{
		"position_mode": modeStr,
	}
	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return err
	}
	_, err = c.requestFull(ctx, http.MethodPost, "/contract/private/set-position-mode", nil, bodyBytes, true)
	return err
}
