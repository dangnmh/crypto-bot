package okx

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

// Explicit request/response structs for order endpoints.

type okxAttachAlgoOrd struct {
	AttachAlgoClOrdId    string `json:"attachAlgoClOrdId,omitempty"`
	TpTriggerPx          string `json:"tpTriggerPx,omitempty"`
	TpTriggerRatio       string `json:"tpTriggerRatio,omitempty"`
	TpOrdPx              string `json:"tpOrdPx,omitempty"`
	TpOrdKind            string `json:"tpOrdKind,omitempty"`
	SlTriggerPx          string `json:"slTriggerPx,omitempty"`
	SlTriggerRatio       string `json:"slTriggerRatio,omitempty"`
	SlOrdPx              string `json:"slOrdPx,omitempty"`
	TpTriggerPxType      string `json:"tpTriggerPxType,omitempty"`
	SlTriggerPxType      string `json:"slTriggerPxType,omitempty"`
	Sz                   string `json:"sz,omitempty"`
	AmendPxOnTriggerType string `json:"amendPxOnTriggerType,omitempty"`
	CallbackRatio        string `json:"callbackRatio,omitempty"`
	CallbackSpread       string `json:"callbackSpread,omitempty"`
	ActivePx             string `json:"activePx,omitempty"`
}

type okxCreateOrderRequest struct {
	InstID           string             `json:"instId"`
	TdMode           string             `json:"tdMode"`
	Ccy              string             `json:"ccy,omitempty"`
	ClOrdID          string             `json:"clOrdId,omitempty"`
	Tag              string             `json:"tag,omitempty"`
	Side             string             `json:"side"`
	PosSide          string             `json:"posSide,omitempty"`
	OrdType          string             `json:"ordType"`
	Sz               string             `json:"sz"`
	Px               string             `json:"px,omitempty"`
	SpeedBump        string             `json:"speedBump,omitempty"`
	Outcome          string             `json:"outcome,omitempty"`
	PxUsd            string             `json:"pxUsd,omitempty"`
	PxVol            string             `json:"pxVol,omitempty"`
	ReduceOnly       bool               `json:"reduceOnly,omitempty"`
	TgtCcy           string             `json:"tgtCcy,omitempty"`
	BanAmend         bool               `json:"banAmend,omitempty"`
	PxAmendType      string             `json:"pxAmendType,omitempty"`
	TradeQuoteCcy    string             `json:"tradeQuoteCcy,omitempty"`
	SlippagePct      string             `json:"slippagePct,omitempty"`
	StpMode          string             `json:"stpMode,omitempty"`
	IsElpTakerAccess bool               `json:"isElpTakerAccess,omitempty"`
	AttachAlgoOrds   []okxAttachAlgoOrd `json:"attachAlgoOrds,omitempty"`
}

type okxCreateOrderResult struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	Tag     string `json:"tag"`
	Ts      string `json:"ts"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
	SubCode string `json:"subCode"`
}

type okxCancelOrderRequest struct {
	InstID string `json:"instId"`
	OrdID  string `json:"ordId"`
}

type okxCancelOrderResult struct {
	OrdID   string `json:"ordId"`
	ClOrdID string `json:"clOrdId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

type okxOrdersRequest struct {
	InstType string `json:"instType"`
	InstID   string `json:"instId,omitempty"`
}

type okxOrderDetailRequest struct {
	InstID  string `json:"instId"`
	OrdID   string `json:"ordId,omitempty"`
	ClOrdID string `json:"clOrdId,omitempty"`
}

type okxOrder struct {
	InstID    string `json:"instId"`
	OrdID     string `json:"ordId"`
	ClOrdID   string `json:"clOrdId"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	Side      string `json:"side"`
	PosSide   string `json:"posSide"`
	State     string `json:"state"`
	OrdType   string `json:"ordType"`
	AccRe     string `json:"accRe"`
	AvgPx     string `json:"avgPx"`
	UTime     string `json:"uTime"`
	CTime     string `json:"cTime"`
	FillSz    string `json:"fillSz"`
	AccFillSz string `json:"accFillSz"`
	TradeId   string `json:"tradeId"`
}

// Private raw methods invoking the OKX V5 REST API.

func (c *Client) rawCreateOrder(ctx context.Context, req okxCreateOrderRequest) (*okxCreateOrderResult, error) {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("okx marshal create order request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, pathPlaceOrder, nil, bodyBytes)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxCreateOrderResult](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawCancelOrder(ctx context.Context, req okxCancelOrderRequest) (*okxCancelOrderResult, error) {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("okx marshal cancel order request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, pathCancelOrder, nil, bodyBytes)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[okxCancelOrderResult](body, "cancel_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawGetOpenOrders(ctx context.Context, req okxOrdersRequest) ([]okxOrder, error) {
	params := map[string]string{
		paramInstType: req.InstType,
	}
	if req.InstID != "" {
		params[paramInstId] = req.InstID
	}
	if params[paramInstType] == "" {
		params[paramInstType] = instTypeSwap
	}
	body, err := c.RawRequest(ctx, http.MethodGet, "/api/v5/trade/orders-pending", params, nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[okxOrder](body, "open_orders")
}

func (c *Client) rawGetOrderDetail(ctx context.Context, req okxOrderDetailRequest) (*okxOrder, error) {
	params := map[string]string{
		"instId": req.InstID,
	}
	if req.ClOrdID != "" {
		params["clOrdId"] = req.ClOrdID
	}
	body, err := c.GetOrderDetailRaw(ctx, req.OrdID, params)
	if err != nil {
		return nil, err
	}
	resList, err := ParseResponse[okxOrder](body, "order_detail")
	if err != nil {
		return nil, err
	}
	if len(resList) == 0 {
		return nil, nil
	}
	return &resList[0], nil
}

// Public mapper methods implementing the exchange.OrderExecutor interface.

// CreateOrder submits a new order and returns the order ID.
func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	ordType := mapOKXOrderType(req.Type)
	isHedge := req.PositionMode == 1 || req.PositionMode == 0
	side, posSide := mapOKXOrderSide(req.Side, isHedge)

	tdMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		tdMode = modeCross
	}

	okxReq := okxCreateOrderRequest{
		InstID:     req.Symbol,
		TdMode:     tdMode,
		Side:       side,
		OrdType:    ordType,
		Sz:         decmath.FormatFloat(req.Vol),
		ReduceOnly: req.ReduceOnly,
	}

	if isHedge {
		okxReq.PosSide = posSide
	}

	if req.Type != exchange.OrderTypeMarket {
		okxReq.Px = decmath.FormatFloat(req.Price)
	}

	if req.ExternalOID != "" {
		okxReq.ClOrdID = req.ExternalOID
	}

	if req.TakeProfitPrice > 0 || req.StopLossPrice > 0 {
		algo := okxAttachAlgoOrd{}
		if req.TakeProfitPrice > 0 {
			algo.TpTriggerPx = decmath.FormatFloat(req.TakeProfitPrice)
			algo.TpOrdPx = "-1"
			algo.TpTriggerPxType = triggerPxTypeLast
			algo.TpOrdKind = "condition"
		}
		if req.StopLossPrice > 0 {
			algo.SlTriggerPx = decmath.FormatFloat(req.StopLossPrice)
			algo.SlOrdPx = "-1"
			algo.SlTriggerPxType = triggerPxTypeLast
		}
		okxReq.AttachAlgoOrds = []okxAttachAlgoOrd{algo}
	}

	res, err := c.rawCreateOrder(ctx, okxReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	if res.SCode != "0" {
		codeVal := 0
		_, _ = fmt.Sscanf(res.SCode, "%d", &codeVal)
		return exchange.CreateOrderResult{}, toAPIError(codeVal, res.SMsg, "create_order")
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{
		OrderID:       res.OrdID,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

func mapOKXOrderType(t domain.OrderType) string {
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

func mapOKXOrderSide(s domain.Side, isHedge bool) (string, string) {
	if isHedge {
		switch s {
		case exchange.SideOpenLong:
			return sideBuy, posSideLong
		case exchange.SideCloseLong:
			return sideSell, posSideLong
		case exchange.SideOpenShort:
			return sideSell, posSideShort
		case exchange.SideCloseShort:
			return sideBuy, posSideShort
		default:
			return sideBuy, posSideLong
		}
	}

	posSide := posSideNet
	side := sideBuy
	switch s {
	case exchange.SideOpenLong, exchange.SideCloseShort:
		side = sideBuy
	case exchange.SideOpenShort, exchange.SideCloseLong:
		side = sideSell
	default:
		// SideUnknown or unhandled
	}
	return side, posSide
}

// CancelOrder cancels a single order by its ID.
func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	if symbol == "" {
		return fmt.Errorf("cancel order failed: symbol is required")
	}

	res, err := c.rawCancelOrder(ctx, okxCancelOrderRequest{
		InstID: symbol,
		OrdID:  orderID,
	})
	if err != nil {
		return err
	}

	if res.SCode != "0" {
		// If already cancelled or filled, treat as success to match expected behavior.
		if res.SCode == "51400" || res.SCode == "51401" || strings.Contains(strings.ToLower(res.SMsg), "already") || strings.Contains(strings.ToLower(res.SMsg), "filled") {
			return nil
		}
		codeVal := 0
		_, _ = fmt.Sscanf(res.SCode, "%d", &codeVal)
		return toAPIError(codeVal, res.SMsg, "cancel_order")
	}

	return nil
}

// CancelOrders is a stub.
func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("batch cancel not supported on OKX without symbols")
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

// GetOrder queries a single order by exchange order ID.
func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	if symbol == "" {
		return nil, fmt.Errorf("query order failed: symbol is required")
	}
	res, err := c.rawGetOrderDetail(ctx, okxOrderDetailRequest{
		InstID: symbol,
		OrdID:  orderID,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	info := mapOkxOrder(*res)
	return &info, nil
}

// GetOrderByExternalID queries a single order by client order ID.
func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	if symbol == "" {
		return nil, fmt.Errorf("query order by external ID failed: symbol is required")
	}
	res, err := c.rawGetOrderDetail(ctx, okxOrderDetailRequest{
		InstID:  symbol,
		ClOrdID: externalOrderID,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, fmt.Errorf("order not found by external ID: %s", externalOrderID)
	}
	info := mapOkxOrder(*res)
	return &info, nil
}

// GetOpenOrders returns all open orders.
func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	rawList, err := c.rawGetOpenOrders(ctx, okxOrdersRequest{InstType: instTypeSwap, InstID: symbol})
	if err != nil {
		return nil, err
	}

	orders := make([]exchange.OrderInfo, 0, len(rawList))
	for i := range rawList {
		orders = append(orders, mapOkxOrder(rawList[i]))
	}

	return orders, nil
}

// ClosePosition closes one position leg using a market order.

func mapOkxOrder(o okxOrder) exchange.OrderInfo {
	px, _ := strconv.ParseFloat(o.Px, 64)
	sz, _ := strconv.ParseFloat(o.Sz, 64)
	avgPx, _ := strconv.ParseFloat(o.AvgPx, 64)
	fillSzStr := o.AccFillSz
	if fillSzStr == "" {
		fillSzStr = o.FillSz
	}
	fillSz, _ := strconv.ParseFloat(fillSzStr, 64)
	uTime, _ := strconv.ParseInt(o.UTime, 10, 64)
	cTime, _ := strconv.ParseInt(o.CTime, 10, 64)

	info := exchange.OrderInfo{
		OrderID:      o.OrdID,
		Symbol:       o.InstID,
		Price:        px,
		Vol:          sz,
		DealAvgPrice: avgPx,
		DealVol:      fillSz,
		ExternalOID:  o.ClOrdID,
		CreateTime:   cTime,
		UpdateTime:   uTime,
		PositionMode: domain.PositionModeOneWay, // default OneWay
	}

	switch o.PosSide {
	case posSideLong:
		info.PositionMode = domain.PositionModeHedge
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideCloseLong
		}
	case posSideShort:
		info.PositionMode = domain.PositionModeHedge
		if o.Side == sideSell {
			info.Side = exchange.SideOpenShort
		} else {
			info.Side = exchange.SideCloseShort
		}
	default:
		// net mode
		if o.Side == sideBuy {
			info.Side = exchange.SideOpenLong
		} else {
			info.Side = exchange.SideOpenShort
		}
	}

	switch o.State {
	case stateLive:
		info.State = exchange.OrderStateNew
	case statePartFill:
		info.State = exchange.OrderStatePartiallyFilled
	case stateFilled:
		info.State = exchange.OrderStateFilled
	case stateCanceled, stateMmpCanceled:
		info.State = exchange.OrderStateCanceled
	default:
		info.State = exchange.OrderStateNew
	}

	return info
}
