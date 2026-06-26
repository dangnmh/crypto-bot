package deepcoin

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"crypto-bot/pkg/xjson"
)

type deepcoinOrderReq struct {
	InstID      string `json:"instId"`
	TdMode      string `json:"tdMode"`
	Side        string `json:"side"`
	PosSide     string `json:"posSide,omitempty"`
	MrgPosition string `json:"mrgPosition,omitempty"`
	OrdType     string `json:"ordType"`
	Sz          string `json:"sz"`
	Px          string `json:"px,omitempty"`
	ClOrdId     string `json:"clOrdId,omitempty"`
	ReduceOnly  bool   `json:"reduceOnly,omitempty"`
	TpTriggerPx string `json:"tpTriggerPx,omitempty"`
	SlTriggerPx string `json:"slTriggerPx,omitempty"`
}

type deepcoinOrderResultData struct {
	OrdId   string `json:"ordId"`
	ClOrdId string `json:"clOrdId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

type deepcoinOrder struct {
	InstID    string       `json:"instId"`
	OrdId     string       `json:"ordId"`
	ClOrdId   string       `json:"clOrdId"`
	Px        xjson.Number `json:"px"`
	Sz        xjson.Number `json:"sz"`
	OrdType   string       `json:"ordType"`
	Side      string       `json:"side"`
	PosSide   string       `json:"posSide"`
	AccFillSz xjson.Number `json:"accFillSz"`
	AvgPx     xjson.Number `json:"avgPx"`
	State     string       `json:"state"`
}

func mapDeepcoinOrderSideAndPosition(reqSide domain.Side) (string, string, bool) {
	side := sideBuy
	posSide := posSideLong
	if reqSide == exchange.SideOpenShort || reqSide == exchange.SideCloseShort {
		posSide = posSideShort
	}
	if reqSide == exchange.SideCloseLong || reqSide == exchange.SideOpenShort {
		side = sideSell
	}
	reduceOnly := reqSide == exchange.SideCloseLong || reqSide == exchange.SideCloseShort
	return side, posSide, reduceOnly
}

func mapDeepcoinOrderType(t domain.OrderType) string {
	switch t {
	case domain.OrderTypeLimit, domain.OrderTypeFOK:
		return ordTypeLimit
	case domain.OrderTypeMarket:
		return ordTypeMarket
	case domain.OrderTypePostOnly:
		return "post_only"
	case domain.OrderTypeIOC:
		return "ioc"
	default:
		return ordTypeLimit
	}
}

func (c *Client) CreateOrder(ctx context.Context, req exchange.SubmitOrderRequest) (exchange.CreateOrderResult, error) {
	side, posSide, reduceOnly := mapDeepcoinOrderSideAndPosition(req.Side)
	ordType := mapDeepcoinOrderType(req.Type)

	tdMode := mgnModeCross
	if req.OpenType == domain.OpenTypeIsolated {
		tdMode = mgnModeIsolated
	}

	rawReq := deepcoinOrderReq{
		InstID:      req.Symbol,
		TdMode:      tdMode,
		Side:        side,
		PosSide:     posSide,
		MrgPosition: mrgPositionMerge,
		OrdType:     ordType,
		Sz:          decmath.FormatFloat(req.Vol),
		ClOrdId:     req.ExternalOID,
		ReduceOnly:  reduceOnly,
	}
	if req.Type != exchange.OrderTypeMarket {
		rawReq.Px = decmath.FormatFloat(req.Price)
	}

	if req.TakeProfitPrice > 0 {
		rawReq.TpTriggerPx = decmath.FormatFloat(req.TakeProfitPrice)
	}
	if req.StopLossPrice > 0 {
		rawReq.SlTriggerPx = decmath.FormatFloat(req.StopLossPrice)
	}

	body, err := c.PostCtx(ctx, "/deepcoin/trade/order", rawReq)
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}

	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "create_order")
	if err != nil {
		return exchange.CreateOrderResult{}, err
	}
	if res.SCode != "0" {
		return exchange.CreateOrderResult{}, fmt.Errorf("create order returned code %s: %s", res.SCode, res.SMsg)
	}

	tpslSubmitted := req.TakeProfitPrice > 0 || req.StopLossPrice > 0
	return exchange.CreateOrderResult{
		OrderID:       res.OrdId,
		TPSLSubmitted: tpslSubmitted,
	}, nil
}

func (c *Client) CancelOrder(ctx context.Context, symbol, orderID string) error {
	bodyMap := map[string]string{
		paramInstId: symbol,
		paramOrdId:  orderID,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/trade/cancel-order", bodyMap)
	if err != nil {
		return err
	}
	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "cancel_order")
	if err != nil {
		return err
	}
	if res.SCode != "0" {
		return fmt.Errorf("cancel order failed (code %s): %s", res.SCode, res.SMsg)
	}
	return nil
}

func (c *Client) CancelOrders(ctx context.Context, orderIDs []string) error {
	return fmt.Errorf("CancelOrders not supported on Deepcoin")
}

func (c *Client) CancelAllOpenOrders(ctx context.Context, symbol string) error {
	bodyMap := map[string]any{
		"InstrumentID":  normalizeSymbol(symbol),
		"ProductGroup":  "SwapU",
		"IsCrossMargin": 1,
		"IsMergeMode":   1,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/trade/swap/cancel-all", bodyMap)
	if err != nil {
		return err
	}
	type cancelAllData struct {
		ErrorList []any `json:"errorList"`
	}
	_, err = ParseResponseFirst[cancelAllData](body, "cancel_all")
	return err
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		paramInstId: symbol,
		paramOrdId:  orderID,
	}
	body, err := c.GetCtx(ctx, "/deepcoin/trade/order", params)
	if err != nil {
		return nil, err
	}
	orders, err := ParseResponse[deepcoinOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	return c.toOrderInfo(&orders[0]), nil
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	params := map[string]string{
		paramInstId: symbol,
		"clOrdId":   externalOrderID,
	}
	body, err := c.GetCtx(ctx, "/deepcoin/trade/order", params)
	if err != nil {
		return nil, err
	}
	orders, err := ParseResponse[deepcoinOrder](body, "get_order")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("order not found by client ID: %s", externalOrderID)
	}
	return c.toOrderInfo(&orders[0]), nil
}

func (c *Client) GetOpenOrders(ctx context.Context, symbol string) ([]exchange.OrderInfo, error) {
	return nil, fmt.Errorf("GetOpenOrders not supported on Deepcoin")
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
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
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "deepcoin"),
	})
	return err
}

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
		_ = c.ClosePosition(ctx, symbol, closeSide, pos.HoldVol, 1, pos.Leverage)
	}
	return nil
}

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mgnType := mgnModeCross
	if req.OpenType == domain.OpenTypeIsolated {
		mgnType = mgnModeIsolated
	}
	bodyMap := map[string]any{
		paramInstId:      req.Symbol,
		paramLever:       strconv.Itoa(req.Leverage),
		paramMgnMode:     mgnType,
		paramMrgPosition: mrgPositionMerge,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/account/set-leverage", bodyMap)
	if err != nil {
		return err
	}
	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "set_leverage")
	if err != nil {
		return err
	}
	if res.SCode != "0" {
		return fmt.Errorf("set leverage failed: %s", res.SMsg)
	}
	return nil
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}

func (c *Client) toOrderInfo(o *deepcoinOrder) *exchange.OrderInfo {
	var state domain.OrderState
	switch o.State {
	case stateLive, "partially_filled":
		state = exchange.OrderStateNew
	case "filled":
		state = exchange.OrderStateFilled
	case "canceled":
		state = exchange.OrderStateCanceled
	default:
		state = exchange.OrderStateNew
	}

	var sideVal domain.Side
	if o.Side == sideBuy {
		if o.PosSide == posSideLong {
			sideVal = exchange.SideOpenLong
		} else {
			sideVal = exchange.SideCloseShort
		}
	} else {
		if o.PosSide == posSideLong {
			sideVal = exchange.SideCloseLong
		} else {
			sideVal = exchange.SideOpenShort
		}
	}

	px, _ := strconv.ParseFloat(string(o.Px), 64)
	sz, _ := strconv.ParseFloat(string(o.Sz), 64)
	fillSz, _ := strconv.ParseFloat(string(o.AccFillSz), 64)
	avgPx, _ := strconv.ParseFloat(string(o.AvgPx), 64)

	return &exchange.OrderInfo{
		OrderID:      o.OrdId,
		Symbol:       o.InstID,
		Price:        px,
		Vol:          sz,
		DealVol:      fillSz,
		DealAvgPrice: avgPx,
		State:        state,
		ExternalOID:  o.ClOrdId,
		Side:         sideVal,
	}
}
