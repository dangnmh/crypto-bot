package bitget

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

type bitgetOrderResult struct {
	OrderID   string `json:"orderId"`
	ClientOid string `json:"clientOid"`
}

type bitgetOrder struct {
	OrderID    string `json:"orderId"`
	ClientOid  string `json:"clientOid"`
	Symbol     string `json:"symbol"`
	Size       string `json:"size"`
	Price      string `json:"price"`
	PriceAvg   string `json:"priceAvg"`
	BaseVolume string `json:"baseVolume"`
	State      string `json:"state"`
	Side       string `json:"side"`
	PosSide    string `json:"posSide"`
	Leverage   string `json:"leverage"`
	CTime      string `json:"cTime"`
	UTime      string `json:"uTime"`
}

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (string, error) {
	ordType := mapBitgetOrderType(req.Type)
	isHedge := req.PositionMode == 1 || req.PositionMode == 0
	side, tradeSide := mapBitgetOrderSide(req.Side, isHedge)

	marginMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		marginMode = modeCrossed
	}

	bodyMap := map[string]interface{}{
		paramSymbol:      req.Symbol,
		paramProductType: productTypeUsdtFutures,
		paramMarginMode:  marginMode,
		"side":           side,
		"orderType":      ordType,
		"size":           fmt.Sprintf("%g", req.Vol),
		paramMarginCoin:  constantUsdt,
	}

	if isHedge {
		bodyMap["tradeSide"] = tradeSide
	}

	if req.Type != exchange.OrderTypeMarket {
		bodyMap["price"] = fmt.Sprintf("%g", req.Price)
	}

	if req.ExternalOID != "" {
		bodyMap["clientOid"] = req.ExternalOID
	}

	if req.ReduceOnly {
		bodyMap["reduceOnly"] = true
	}

	body, err := c.PostCtx(ctx, pathPlaceOrder, bodyMap)
	if err != nil {
		return "", err
	}

	res, err := ParseResponse[bitgetOrderResult](body, "create_order")
	if err != nil {
		return "", err
	}

	if res.OrderID == "" && req.ExternalOID != "" {
		return req.ExternalOID, nil
	}

	return res.OrderID, nil
}

// CreateTrackOrder submits a trailing stop order (stubbed/not implemented).
func (c *Client) CreateTrackOrder(ctx context.Context, req exchange.SubmitTrackOrderRequest) (string, error) {
	return "", fmt.Errorf("CreateTrackOrder not implemented for Bitget")
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if symbol == "" {
		info, err := c.GetOrder(ctx, orderID)
		if err != nil {
			return fmt.Errorf("cancel order failed to locate order: %w", err)
		}
		symbol = info.Symbol
	}

	bodyMap := map[string]interface{}{
		paramSymbol:      symbol,
		paramProductType: productTypeUsdtFutures,
		"orderId":        orderID,
	}

	body, err := c.PostCtx(ctx, pathCancelOrder, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "cancel_order")
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for i := range orderIDs {
		err := c.CancelOrder(ctx, "", orderIDs[i])
		if err != nil {
			return err
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
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

// GetOrder queries a single order by ID.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*exchange.OrderInfo, error) {
	// Query pending orders
	pendingBody, err := c.GetCtx(ctx, pathPendingOrders, map[string]string{paramProductType: productTypeUsdtFutures})
	if err == nil {
		pendingList, parseErr := ParseResponse[[]bitgetOrder](pendingBody, "pending_orders")
		if parseErr == nil {
			for i := range pendingList {
				o := pendingList[i]
				if o.OrderID == orderID || o.ClientOid == orderID {
					info := mapBitgetOrder(o)
					return &info, nil
				}
			}
		}
	}

	// Historical order detail can only be queried if we have the symbol
	// Since we don't have symbol here, let's fetch orders-history and scan
	historyBody, err := c.GetCtx(ctx, "/api/v2/mix/order/orders-history", map[string]string{paramProductType: productTypeUsdtFutures})
	if err == nil {
		historyList, parseErr := ParseResponse[[]bitgetOrder](historyBody, "orders_history")
		if parseErr == nil {
			for i := range historyList {
				o := historyList[i]
				if o.OrderID == orderID || o.ClientOid == orderID {
					info := mapBitgetOrder(o)
					return &info, nil
				}
			}
		}
	}

	return nil, fmt.Errorf("order not found: %s", orderID)
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, pathPendingOrders, params)
	if err != nil {
		return nil, err
	}

	list, err := ParseResponse[[]bitgetOrder](body, "open_orders")
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(list))
	for i := range list {
		orders = append(orders, mapBitgetOrder(list[i]))
	}

	return orders, nil
}

// ClosePosition closes one position leg using a market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         int(closeSide),
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := positions[i]
		if pos.HoldVol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == 1 { // Long
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, pos.HoldVol, 1)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	bodyMap := map[string]interface{}{
		paramSymbol:      req.Symbol,
		paramProductType: productTypeUsdtFutures,
		paramMarginCoin:  constantUsdt,
		paramLeverage:    strconv.Itoa(req.Leverage),
	}

	// Specify long/short leverage as well for hedge mode isolation safety
	bodyMap["longLeverage"] = strconv.Itoa(req.Leverage)
	bodyMap["shortLeverage"] = strconv.Itoa(req.Leverage)

	body, err := c.PostCtx(ctx, pathSetLeverage, bodyMap)
	if err != nil {
		return err
	}

	return ParseResponseIgnoreData(body, "change_leverage")
}

func mapBitgetOrder(o bitgetOrder) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(o.Price, 64)
	sz, _ := strconv.ParseFloat(o.Size, 64)
	avgPx, _ := strconv.ParseFloat(o.PriceAvg, 64)
	fillSz, _ := strconv.ParseFloat(o.BaseVolume, 64)

	cTimeVal := decmath.ParseInt64(o.CTime)
	uTimeVal := decmath.ParseInt64(o.UTime)
	leverageVal := decmath.ParseInt64(o.Leverage)

	info := exchange.OrderInfo{
		OrderID:      o.OrderID,
		Symbol:       o.Symbol,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClientOid,
		CreateTime:   cTimeVal,
		UpdateTime:   uTimeVal,
		Leverage:     int(leverageVal),
		PositionMode: 2, // default OneWay
	}

	switch o.PosSide {
	case posSideLong:
		info.PositionMode = 1
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideCloseLong
		}
	case posSideShort:
		info.PositionMode = 1
		if o.Side == sideSell {
			info.Side = exchange.SideOpenShort
		} else {
			info.Side = exchange.SideCloseShort
		}
	default:
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	switch o.State {
	case stateFilled:
		info.State = exchange.OrderStateFilled
	case stateCanceled:
		info.State = exchange.OrderStateCanceled
	default:
		info.State = exchange.OrderStatePartial
	}

	return info
}

func mapBitgetOrderType(t int) string {
	switch t {
	case exchange.OrderTypeMarket:
		return "market"
	case exchange.OrderTypePostOnly:
		return "post_only"
	case exchange.OrderTypeIOC:
		return "ioc"
	case exchange.OrderTypeFOK:
		return "fok"
	default:
		return paramLimit
	}
}

func mapBitgetOrderSide(s int, isHedge bool) (string, string) {
	if isHedge {
		switch s {
		case exchange.SideOpenLong:
			return sideBuy, sideOpen
		case exchange.SideCloseLong:
			return sideSell, sideClose
		case exchange.SideOpenShort:
			return sideSell, sideOpen
		case exchange.SideCloseShort:
			return sideBuy, sideClose
		default:
			return sideBuy, sideOpen
		}
	}

	switch s {
	case exchange.SideOpenLong, exchange.SideCloseShort:
		return sideBuy, sideOpen
	case exchange.SideOpenShort, exchange.SideCloseLong:
		return sideSell, sideOpen
	default:
		return sideBuy, sideOpen
	}
}
