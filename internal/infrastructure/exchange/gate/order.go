package gate

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

// Explicit request/response structs for order endpoints.

type gateChangeLeverageRequest struct {
	Symbol             string `json:"symbol"`
	Leverage           string `json:"leverage"`
	CrossLeverageLimit string `json:"cross_leverage_limit,omitempty"`
}

type gatePositionCrossModeRequest struct {
	Mode     string `json:"mode"`
	Contract string `json:"contract"`
}

// Private raw methods invoking raw HTTP requests.

func (c *Client) createRawOrder(ctx context.Context, settle string, order gateFuturesOrder) (*gateFuturesOrder, error) {
	bodyBytes, err := xjson.Marshal(order)
	if err != nil {
		return nil, fmt.Errorf("gate marshal order: %w", err)
	}
	path := fmt.Sprintf("/futures/%s/orders", settle)
	body, err := c.RawRequest(ctx, "POST", path, nil, bodyBytes)
	if err != nil {
		return nil, err
	}
	var result gateFuturesOrder
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return &result, nil
}

func (c *Client) cancelRawOrder(ctx context.Context, settle, orderID string) error {
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)
	_, err := c.RawRequest(ctx, "DELETE", path, nil, nil)
	return err
}

func (c *Client) cancelRawAllOpenOrders(ctx context.Context, settle, symbol string) error {
	params := map[string]string{
		paramContract: symbol,
	}
	path := fmt.Sprintf("/futures/%s/orders", settle)
	_, err := c.RawRequest(ctx, "DELETE", path, params, nil)
	return err
}

func (c *Client) getRawOrder(ctx context.Context, settle, orderID string) (*gateFuturesOrder, error) {
	params := map[string]string{
		paramSettle: settle,
	}
	body, err := c.GetOrderDetailRaw(ctx, orderID, params)
	if err != nil {
		return nil, err
	}
	var result gateFuturesOrder
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return &result, nil
}

func (c *Client) getRawOpenOrders(ctx context.Context, settle, symbol string) ([]gateFuturesOrder, error) {
	return c.getRawOrdersByStatus(ctx, settle, symbol, "open")
}

func (c *Client) getRawOrdersByStatus(ctx context.Context, settle, symbol, status string) ([]gateFuturesOrder, error) {
	params := map[string]string{
		paramSettle:   settle,
		paramContract: symbol,
		"status":      status,
	}
	if settle == "" {
		settle = gateSettleUsdt
	}
	path := fmt.Sprintf("/futures/%s/orders", settle)
	body, err := c.RawRequest(ctx, "GET", path, params, nil)
	if err != nil {
		return nil, err
	}
	var result []gateFuturesOrder
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) getRawOrdersTimerange(ctx context.Context, settle, symbol string, fromTime time.Time) ([]gateFuturesOrderTimerange, error) {
	params := map[string]string{
		paramContract: symbol,
	}
	if !fromTime.IsZero() {
		params["from"] = strconv.FormatInt(fromTime.UnixMilli(), 10)
	}
	path := fmt.Sprintf("/futures/%s/orders_timerange", settle)
	body, err := c.RawRequest(ctx, "GET", path, params, nil)
	if err != nil {
		return nil, err
	}
	var result []gateFuturesOrderTimerange
	if err := xjson.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to unmarshal gate response: %w", err)
	}
	return result, nil
}

func (c *Client) changeRawLeverage(ctx context.Context, settle string, req gateChangeLeverageRequest) error {
	params := map[string]string{
		"leverage": req.Leverage,
	}
	if req.CrossLeverageLimit != "" {
		params["cross_leverage_limit"] = req.CrossLeverageLimit
	}

	path := fmt.Sprintf("/futures/%s/positions/%s/leverage", settle, req.Symbol)
	_, err := c.RawRequest(ctx, "POST", path, params, nil)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/%s/leverage", settle, req.Symbol)
		_, errDual := c.RawRequest(ctx, "POST", dualPath, params, nil)
		if errDual != nil {
			return fmt.Errorf("gate.io update leverage error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	order := c.mapSubmitOrder(req)
	resp, err := c.createRawOrder(ctx, gateSettleUsdt, order)
	if err != nil {
		return exchange.CreateOrderResult{}, fmt.Errorf("gate.io create order: %w", err)
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{OrderID: strconv.FormatInt(resp.Id, 10), TPSLSubmitted: tpslSubmitted}, nil
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	err := c.cancelRawOrder(ctx, gateSettleUsdt, orderID)
	if err != nil {
		return fmt.Errorf("gate.io cancel order: %w", err)
	}
	return nil
}

// CancelOrders cancels multiple orders.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	for _, id := range orderIDs {
		err := c.cancelRawOrder(ctx, gateSettleUsdt, id)
		if err != nil {
			return fmt.Errorf("gate.io cancel bulk order %s: %w", id, err)
		}
	}
	return nil
}

// CancelAllOpenOrders cancels all open orders for a given symbol.
func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	err := c.cancelRawAllOpenOrders(ctx, gateSettleUsdt, symbol)
	if err != nil {
		return fmt.Errorf("gate.io cancel all open orders for %s: %w", symbol, err)
	}
	return nil
}

// GetOrder retrieves detailed information about a specific order by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	resp, err := c.getRawOrder(ctx, gateSettleUsdt, orderID)
	if err != nil {
		// Fallback to checking orders within recent time range if not found by ID directly
		orders, openErr := c.getRawOrdersTimerange(ctx, gateSettleUsdt, symbol, time.Time{})
		if openErr == nil {
			for i := range orders {
				if strconv.FormatInt(orders[i].Id, 10) == orderID {
					mapped := mapOrderTimerangeInfo(orders[i])
					return &mapped, nil
				}
			}
		}
		return nil, fmt.Errorf("gate.io get order by ID %s: %w", orderID, err)
	}

	mapped := mapOrderInfo(*resp)
	return &mapped, nil
}

// GetOrderByExternalID retrieves detailed information about a specific order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	return c.GetOrderByExternalIDWithTime(ctx, symbol, externalOrderID, time.Time{})
}

// GetOrderByExternalIDWithTime retrieves detailed information about a specific order by client order ID with a start time.
func (c *Client) GetOrderByExternalIDWithTime(ctx context.Context, symbol, externalOrderID string, startTime time.Time) (*exchange.OrderInfo, error) {
	targetText := externalOrderID
	if !strings.HasPrefix(targetText, "t-") {
		targetText = "t-" + targetText
	}

	resp, err := c.getRawOrder(ctx, gateSettleUsdt, targetText)
	if err == nil && resp != nil {
		mapped := mapOrderInfo(*resp)
		return &mapped, nil
	}

	// Fallback 1: Query finished orders list first
	finishedOrders, fallback1Err := c.getRawOrdersByStatus(ctx, gateSettleUsdt, symbol, "finished")
	if fallback1Err == nil {
		for i := range finishedOrders {
			if finishedOrders[i].Text == externalOrderID || finishedOrders[i].Text == targetText {
				mapped := mapOrderInfo(finishedOrders[i])
				return &mapped, nil
			}
		}
	}

	// Fallback 2: checking orders within recent time range (historical/timerange)
	orders, fallbackErr := c.getRawOrdersTimerange(ctx, gateSettleUsdt, symbol, startTime)
	if fallbackErr == nil {
		for i := range orders {
			if orders[i].Text == externalOrderID || orders[i].Text == targetText {
				mapped := mapOrderTimerangeInfo(orders[i])
				return &mapped, nil
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("gate.io get order by external ID %s: %w", externalOrderID, err)
	}
	return nil, fmt.Errorf("gate.io get order by external ID %s not found", externalOrderID)
}

// GetOpenOrders retrieves all currently open/active orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	resp, err := c.getRawOpenOrders(ctx, gateSettleUsdt, symbol)
	if err != nil {
		return nil, fmt.Errorf("gate.io list open orders for %s: %w", symbol, err)
	}

	orders := make([]exchange.OrderInfo, 0, len(resp))
	for i := range resp {
		orders = append(orders, mapOrderInfo(resp[i]))
	}
	return orders, nil
}

// ClosePosition submits a market order to close an open position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode) error {
	orderSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		orderSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         orderSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "gate"),
	})
	if err != nil {
		return fmt.Errorf("gate.io close position: %w", err)
	}
	return nil
}

// CloseAllPositions closes all open positions for the given symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := &positions[i]
		if pos.HoldVol > 0 {
			var side domain.Side
			if pos.PositionType == exchange.PositionTypeLong { // Long
				side = domain.SideCloseLong
			} else { // Short
				side = domain.SideCloseShort
			}
			posErr := c.ClosePosition(ctx, symbol, side, pos.HoldVol, domain.PositionModeHedge) // default hedge mode close
			if posErr != nil {
				return posErr
			}
		}
	}
	return nil
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	var leverageStr string
	var crossLimitStr string

	if req.OpenType == exchange.OpenTypeCross {
		leverageStr = "0"
		crossLimitStr = fmt.Sprintf("%d", req.Leverage)
	} else {
		leverageStr = fmt.Sprintf("%d", req.Leverage)
		crossLimitStr = ""
	}

	return c.changeRawLeverage(ctx, gateSettleUsdt, gateChangeLeverageRequest{
		Symbol:             req.Symbol,
		Leverage:           leverageStr,
		CrossLeverageLimit: crossLimitStr,
	})
}

// SetPositionMode sets the position mode (Hedge Mode vs One-Way Mode) on Gate.io.
func (c *Client) SetPositionMode(ctx context.Context, settle string, dualMode bool) error {
	params := map[string]string{
		"dual_mode": strconv.FormatBool(dualMode),
	}
	path := fmt.Sprintf("/futures/%s/dual_mode", settle)
	_, err := c.RawRequest(ctx, "POST", path, params, nil)
	return err
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Gate.io.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	settle := gateSettleUsdt
	modeStr := gateMarginModeIsolated
	if marginMode == gateMarginModeCross {
		modeStr = gateMarginModeCross
	}

	body := gatePositionCrossModeRequest{
		Contract: symbol,
		Mode:     modeStr,
	}
	bodyBytes, err := xjson.Marshal(&body)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/futures/%s/positions/cross_mode", settle)
	_, err = c.RawRequest(ctx, "POST", path, nil, bodyBytes)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/cross_mode", settle)
		_, errDual := c.RawRequest(ctx, "POST", dualPath, nil, bodyBytes)
		if errDual != nil {
			return fmt.Errorf("gate.io update margin mode error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Helper mapping functions.

func mapOrderPriceAndTif(reqType domain.OrderType, price float64) (priceStr, tif string) {
	if reqType == exchange.OrderTypeMarket {
		return "0", gateTifIOC
	}
	priceStr = decmath.FormatFloat(price)
	switch reqType {
	case exchange.OrderTypePostOnly:
		tif = "poc"
	case exchange.OrderTypeIOC:
		tif = gateTifIOC
	case exchange.OrderTypeFOK:
		tif = "fok"
	default:
		tif = "gtc"
	}
	return priceStr, tif
}

func mapOrderSizeAndClose(side domain.Side, vol float64, positionMode domain.PositionMode, reduceOnly bool) (size int64, autoSize string, isClose bool) {
	isHedge := positionMode == domain.PositionModeHedge
	volInt := int64(vol)

	if isHedge {
		switch side {
		case exchange.SideOpenLong:
			size = volInt
		case exchange.SideOpenShort:
			size = -volInt
		case exchange.SideCloseLong:
			size = 0
			autoSize = "close_long"
		case exchange.SideCloseShort:
			size = 0
			autoSize = "close_short"
		default:
			// SideUnknown or unhandled
		}
	} else {
		switch side {
		case exchange.SideOpenLong, exchange.SideCloseShort:
			size = volInt
		case exchange.SideOpenShort, exchange.SideCloseLong:
			size = -volInt
		default:
			// SideUnknown or unhandled
		}
		if reduceOnly {
			isClose = true
		}
	}
	return size, autoSize, isClose
}

// mapSubmitOrder maps a SubmitOrderRequest to gateFuturesOrder.
func (c *Client) mapSubmitOrder(req exchange.SubmitOrderRequest) gateFuturesOrder {
	priceStr, tifVal := mapOrderPriceAndTif(req.Type, req.Price)
	sizeVal, autoSizeVal, closeVal := mapOrderSizeAndClose(req.Side, req.Vol, req.PositionMode, req.ReduceOnly)

	order := gateFuturesOrder{
		Contract: req.Symbol,
		Price:    priceStr,
		Tif:      tifVal,
		Size:     sizeVal,
		AutoSize: autoSizeVal,
	}

	if autoSizeVal != "" {
		order.ReduceOnly = new(true)
	} else {
		if closeVal {
			order.Close = new(true)
		}
		if req.ReduceOnly {
			order.ReduceOnly = new(true)
		}
	}

	if req.ExternalOID != "" {
		order.Text = "t-" + req.ExternalOID
	}

	if req.TakeProfitPrice > 0 {
		order.TpslTpTriggerPrice = decmath.FormatFloat(req.TakeProfitPrice)
		order.TpslTpPriceType = gatePriceTypeLast
	}
	if req.StopLossPrice > 0 {
		order.TpslSlTriggerPrice = decmath.FormatFloat(req.StopLossPrice)
		order.TpslSlPriceType = gatePriceTypeLast
	}

	return order
}

// mapOrderInfo maps a gateFuturesOrder to exchange.OrderInfo.
func mapOrderInfo(raw gateFuturesOrder) exchange.OrderInfo {
	info := exchange.OrderInfo{
		OrderID:      strconv.FormatInt(raw.Id, 10),
		Symbol:       raw.Contract,
		Price:        decmath.ParseFloat(raw.Price),
		Vol:          float64(decmath.AbsInt64(raw.Size)),
		DealAvgPrice: decmath.ParseFloat(raw.FillPrice),
		DealVol:      float64(decmath.AbsInt64(raw.Size) - decmath.AbsInt64(raw.Left)),
		CreateTime:   int64(raw.CreateTime * 1000),
		UpdateTime:   int64(raw.FinishTime * 1000),
	}

	if raw.Size > 0 {
		info.Side = exchange.SideOpenLong
	} else if raw.Size < 0 {
		info.Side = exchange.SideOpenShort
	}

	switch raw.Status {
	case gateOrderStatusFinished:
		switch raw.FinishAs {
		case gateFinishAsFilled:
			info.State = exchange.OrderStateFilled
		case gateFinishAsCancelled, gateTifIOC:
			info.State = exchange.OrderStateCanceled
		}
	case gateOrderStatusOpen:
		if raw.Left < raw.Size {
			info.State = exchange.OrderStatePartiallyFilled
		} else {
			info.State = exchange.OrderStateNew
		}
	}

	if after, ok := strings.CutPrefix(raw.Text, "t-"); ok {
		info.ExternalOID = after
	} else {
		info.ExternalOID = raw.Text
	}

	return info
}

type gateFuturesOrderTimerange struct {
	Id                   int64        `json:"id"`
	User                 int          `json:"user"`
	CreateTime           float64      `json:"create_time"`
	UpdateTime           string       `json:"update_time"`
	FinishTime           string       `json:"finish_time"`
	FinishAs             string       `json:"finish_as"`
	Status               string       `json:"status"`
	Contract             string       `json:"contract"`
	Size                 xjson.Number `json:"size"`
	Iceberg              xjson.Number `json:"iceberg"`
	Price                string       `json:"price"`
	IsClose              bool         `json:"is_close"`
	IsReduceOnly         bool         `json:"is_reduce_only"`
	IsLiq                bool         `json:"is_liq"`
	Tif                  string       `json:"tif"`
	Left                 xjson.Number `json:"left"`
	FillPrice            string       `json:"fill_price"`
	Text                 string       `json:"text"`
	Tkfr                 string       `json:"tkfr"`
	Mkfr                 string       `json:"mkfr"`
	Refu                 int          `json:"refu"`
	StpId                int          `json:"stp_id"`
	StpAct               string       `json:"stp_act"`
	AmendText            string       `json:"amend_text"`
	MarketOrderSlipRatio string       `json:"market_order_slip_ratio"`
	PosMarginMode        string       `json:"pos_margin_mode"`
	TpslTpTriggerPrice   string       `json:"tpsl_tp_trigger_price"`
	TpslSlTriggerPrice   string       `json:"tpsl_sl_trigger_price"`
}

func parseTimerangeTimes(finishTime, updateTime string) (finishTimeMs, updateTimeMs int64) {
	if finishTime != "" {
		if f, err := strconv.ParseFloat(finishTime, 64); err == nil {
			finishTimeMs = int64(f * 1000)
		}
	}

	if updateTime != "" {
		if u, err := strconv.ParseFloat(updateTime, 64); err == nil {
			updateTimeMs = int64(u * 1000)
		}
	} else if finishTimeMs > 0 {
		updateTimeMs = finishTimeMs
	}
	return finishTimeMs, updateTimeMs
}

func mapOrderTimerangeInfo(raw gateFuturesOrderTimerange) exchange.OrderInfo {
	var sizeVal int64
	if s, err := raw.Size.Int64(); err == nil {
		sizeVal = s
	}
	var leftVal int64
	if l, err := raw.Left.Int64(); err == nil {
		leftVal = l
	}
	_, updateTimeMs := parseTimerangeTimes(raw.FinishTime, raw.UpdateTime)

	info := exchange.OrderInfo{
		OrderID:      strconv.FormatInt(raw.Id, 10),
		Symbol:       raw.Contract,
		Price:        decmath.ParseFloat(raw.Price),
		Vol:          float64(decmath.AbsInt64(sizeVal)),
		DealAvgPrice: decmath.ParseFloat(raw.FillPrice),
		DealVol:      float64(decmath.AbsInt64(sizeVal) - decmath.AbsInt64(leftVal)),
		CreateTime:   int64(raw.CreateTime * 1000),
		UpdateTime:   updateTimeMs,
	}

	if sizeVal > 0 {
		info.Side = exchange.SideOpenLong
	} else if sizeVal < 0 {
		info.Side = exchange.SideOpenShort
	}

	switch raw.Status {
	case gateOrderStatusFinished:
		switch raw.FinishAs {
		case gateFinishAsFilled:
			info.State = exchange.OrderStateFilled
		case gateFinishAsCancelled, gateTifIOC:
			info.State = exchange.OrderStateCanceled
		default:
			info.State = exchange.OrderStateCanceled
		}
	case gateOrderStatusOpen:
		if decmath.AbsInt64(leftVal) < decmath.AbsInt64(sizeVal) {
			info.State = exchange.OrderStatePartiallyFilled
		} else {
			info.State = exchange.OrderStateNew
		}
	}

	if after, ok := strings.CutPrefix(raw.Text, "t-"); ok {
		info.ExternalOID = after
	} else {
		info.ExternalOID = raw.Text
	}

	return info
}
