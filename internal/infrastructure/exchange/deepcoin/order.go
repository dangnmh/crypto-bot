package deepcoin

import (
	"context"
	"fmt"
	"strconv"

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
	CTime     xjson.Number `json:"cTime"`
}

type deepcoinClosedPosition struct {
	InstID        string       `json:"instId"`
	CloseAvgPx    xjson.Number `json:"closeAvgPx"`
	OpenAvgPx     xjson.Number `json:"openAvgPx"`
	Pnl           xjson.Number `json:"pnl"`
	CloseTotalPos xjson.Number `json:"closeTotalPos"`
	CTime         xjson.Number `json:"cTime"`
	UTime         xjson.Number `json:"uTime"`
	Fee           xjson.Number `json:"fee"`
	FundingFee    xjson.Number `json:"fundingFee"`
	RealizedPnl   xjson.Number `json:"realizedPnl"`
	PosSide       string       `json:"posSide"`
	Direction     string       `json:"direction"`
	Pos           xjson.Number `json:"pos"`
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

// Private raw methods.

func (c *Client) rawCreateOrder(ctx context.Context, req deepcoinOrderReq) (*deepcoinOrderResultData, error) {
	body, err := c.PostCtx(ctx, "/deepcoin/trade/order", req)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "create_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawCancelOrder(ctx context.Context, symbol, orderID string) (*deepcoinOrderResultData, error) {
	bodyMap := map[string]string{
		paramInstId: symbol,
		paramOrdId:  orderID,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/trade/cancel-order", bodyMap)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "cancel_order")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

type deepcoinCancelAllData struct {
	ErrorList []any `json:"errorList"`
}

func (c *Client) rawCancelAllOpenOrders(ctx context.Context, symbol string) (*deepcoinCancelAllData, error) {
	bodyMap := map[string]any{
		"InstrumentID":  symbol,
		"ProductGroup":  instTypeSwapU,
		"IsCrossMargin": 1,
		"IsMergeMode":   1,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/trade/swap/cancel-all", bodyMap)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinCancelAllData](body, "cancel_all")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) rawGetOrder(ctx context.Context, symbol, orderID, clOrdID string) ([]deepcoinOrder, error) {
	params := map[string]string{
		paramInstId: symbol,
	}
	if orderID != "" {
		params[paramOrdId] = orderID
	}
	if clOrdID != "" {
		params["clOrdId"] = clOrdID
	}

	path := "/deepcoin/trade/orderByID"
	if orderID == "" && clOrdID != "" {
		path = "/deepcoin/trade/order"
	}

	body, err := c.GetCtx(ctx, path, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinOrder](body, "get_order")
}

func (c *Client) rawGetClosedPositions(ctx context.Context, symbol string, createTime int64) ([]deepcoinClosedPosition, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
		paramInstId:   symbol,
		"limit":       "10",
	}
	if createTime > 0 {
		params["before"] = strconv.FormatInt(createTime-1000, 10)
	}
	body, err := c.GetCtx(ctx, "/deepcoin/account/positions-history", params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinClosedPosition](body, "positions_history")
}

// Public OrderDataProvider methods.

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

	res, err := c.rawCreateOrder(ctx, rawReq)
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
	res, err := c.rawCancelOrder(ctx, symbol, orderID)
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
	_, err := c.rawCancelAllOpenOrders(ctx, symbol)
	return err
}

func (c *Client) GetOrder(ctx context.Context, symbol, orderID string) (*exchange.OrderInfo, error) {
	orders, err := c.rawGetOrder(ctx, symbol, orderID, "")
	if err != nil {
		return nil, err
	}
	if len(orders) == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}
	return c.toOrderInfo(&orders[0]), nil
}

func (c *Client) GetOrderByExternalID(ctx context.Context, symbol, externalOrderID string) (*exchange.OrderInfo, error) {
	orders, err := c.rawGetOrder(ctx, symbol, "", externalOrderID)
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

func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("deepcoin get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchange.ExchangeDeepcoin,
			Symbol:   symbol,
		}, nil
	}

	positions, err := c.rawGetClosedPositions(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		return nil, fmt.Errorf("query closed positions: %w", err)
	}

	if len(positions) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed position history found for symbol %s", symbol)
	}

	pos := positions[0]
	entryPrice, _ := strconv.ParseFloat(string(pos.OpenAvgPx), 64)
	exitPrice, _ := strconv.ParseFloat(string(pos.CloseAvgPx), 64)
	closedSize, _ := strconv.ParseFloat(string(pos.CloseTotalPos), 64)
	closedPnl, _ := strconv.ParseFloat(string(pos.Pnl), 64)
	feeVal, _ := strconv.ParseFloat(string(pos.Fee), 64)
	fundingFeeVal, _ := strconv.ParseFloat(string(pos.FundingFee), 64)
	netPnlVal, _ := strconv.ParseFloat(string(pos.RealizedPnl), 64)

	cTime, _ := strconv.ParseInt(string(pos.CTime), 10, 64)
	uTime, _ := strconv.ParseInt(string(pos.UTime), 10, 64)
	duration := max(uTime-cTime, 0)

	isLong := pos.PosSide == posSideLong
	switch pos.Direction {
	case "short":
		isLong = false
	case "long":
		isLong = true
	}

	pnlRate := 0.0
	if entryPrice > 0 {
		if !isLong {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		}
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchange.ExchangeDeepcoin,
		Symbol:     pos.InstID,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   closedPnl,
		Fee:        feeVal,
		FundingFee: fundingFeeVal,
		NetPnl:     netPnlVal,
		PnLRate:    pnlRate,
		DurationMs: duration,
	}, nil
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
	cTimeVal, _ := strconv.ParseInt(string(o.CTime), 10, 64)

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
		CreateTime:   cTimeVal,
	}
}
