//nolint:misspell // API compatibility requires "realised" spelling
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

type bitmartTradeRow struct {
	OrderID        string `json:"order_id"`
	TradeID        string `json:"trade_id"`
	Symbol         string `json:"symbol"`
	Side           int    `json:"side"`
	Price          string `json:"price"`
	Vol            string `json:"vol"`
	Profit         bool   `json:"profit"`
	RealizedProfit string `json:"realised_profit"`
	PaidFees       string `json:"paid_fees"`
	Account        string `json:"account"`
	CreateTime     int64  `json:"create_time"`
}

type bitmartTradeResponse struct {
	Code    int               `json:"code"`
	Message string            `json:"message"`
	Data    []bitmartTradeRow `json:"data"`
}

type bitmartTransactionRow struct {
	Symbol  string `json:"symbol"`
	Type    string `json:"type"`
	Amount  string `json:"amount"`
	Asset   string `json:"asset"`
	Account string `json:"account"`
	Time    string `json:"time"`
	TranID  string `json:"tran_id"`
}

type bitmartTransactionResponse struct {
	Code    int                     `json:"code"`
	Message string                  `json:"message"`
	Data    []bitmartTransactionRow `json:"data"`
}

type aggregatedTradeResults struct {
	openVolSum          float64
	openPriceVolSum     float64
	closeVolSum         float64
	closePriceVolSum    float64
	totalRealizedProfit float64
	totalFees           float64
	latestTradeTime     int64
	closeTradesFound    bool
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

// Private raw methods.

func (c *Client) rawGetOpenPositions(ctx context.Context, symbol string) ([]byte, error) {
	query := make(map[string]string)
	if symbol != "" {
		query[paramSymbol] = symbol
	}
	return c.requestFull(ctx, http.MethodGet, "/contract/private/position-v2", query, nil, true)
}

func (c *Client) rawGetOrder(ctx context.Context, query map[string]string) (*bitmartOrderInfo, error) {
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/order", query, nil, true)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Code int              `json:"code"`
		Data bitmartOrderInfo `json:"data"`
	}
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) rawGetTrades(ctx context.Context, symbol string, orderInfo *bitmartOrderInfo) ([]bitmartTradeRow, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	if orderInfo.CreateTime > 0 {
		startTimeSec := (orderInfo.CreateTime / 1000) - 60
		query["start_time"] = strconv.FormatInt(startTimeSec, 10)
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/trades", query, nil, true)
	if err != nil {
		return nil, fmt.Errorf("bitmart get trades failed: %w", err)
	}
	var tradeResp bitmartTradeResponse
	if err := xjson.Unmarshal(body, &tradeResp); err != nil {
		return nil, fmt.Errorf("unmarshal trades response: %w", err)
	}
	return tradeResp.Data, nil
}

func (c *Client) rawGetTransactions(ctx context.Context, symbol string, startTimeMs int64) ([]bitmartTransactionRow, error) {
	query := map[string]string{
		paramSymbol:  symbol,
		"flow_type":  "3", // Funding Fee
		"start_time": strconv.FormatInt(startTimeMs, 10),
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/transaction-history", query, nil, true)
	if err != nil {
		return nil, err
	}
	var resp bitmartTransactionResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *Client) rawSetPositionMode(ctx context.Context, body []byte) ([]byte, error) {
	return c.requestFull(ctx, http.MethodPost, "/contract/private/set-position-mode", nil, body, true)
}

// Public mapper methods.

// GetOpenPositions returns all open futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	body, err := c.rawGetOpenPositions(ctx, symbol)
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
		vol := pos.HoldVolContract
		if vol == 0 {
			vol = pos.HoldVolCoin
		}
		err = c.ClosePosition(ctx, symbol, closeSide, vol, domain.PositionModeHedge, pos.Leverage)
		if err != nil {
			return err
		}
	}

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
	_, err = c.rawSetPositionMode(ctx, bodyBytes)
	return err
}

// GetOrderPNL queries order execution trades and transaction logs to map closed trade metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	orderInfo, err := c.getRawOrderWithFallback(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("bitmart get order by ID %s failed: %w", orderID, err)
	}

	state := mapBitmartState(orderInfo.State)
	dealVol := decmath.ParseFloat(orderInfo.DealSize)

	if state == domain.OrderStateCanceled && dealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	trades, fundingFee, err := c.fetchTradesAndFunding(ctx, symbol, orderInfo)
	if err != nil {
		return nil, err
	}

	closeSide := 3 // CloseLong
	if orderInfo.Side == 4 {
		closeSide = 2 // CloseShort
	}

	agg := c.aggregateOpenCloseTrades(orderID, closeSide, orderInfo.CreateTime, trades)

	if !agg.closeTradesFound || agg.closeVolSum == 0 {
		return nil, fmt.Errorf("no closing trades found for symbol %s and opening order %s", symbol, orderID)
	}

	var entryPrice float64
	if agg.openVolSum > 0 {
		entryPrice = agg.openPriceVolSum / agg.openVolSum
	} else {
		entryPrice = decmath.ParseFloat(orderInfo.DealAvgPrice)
		if entryPrice == 0 {
			entryPrice = decmath.ParseFloat(orderInfo.Price)
		}
	}

	exitPrice := agg.closePriceVolSum / agg.closeVolSum
	closedSize := agg.closeVolSum

	var duration int64
	if agg.latestTradeTime > orderInfo.CreateTime {
		duration = agg.latestTradeTime - orderInfo.CreateTime
	}

	var pnlRate float64
	if entryPrice > 0 {
		if orderInfo.Side == 1 {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	return &exchange.ClosedPnLInfo{
		Exchange:           exchangeName,
		Symbol:             symbol,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosedSizeContract: new(closedSize),
		GrossPnL:           agg.totalRealizedProfit,
		Fee:                agg.totalFees,
		FundingFee:         fundingFee,
		DurationMs:         duration,
		NetPnl:             agg.totalRealizedProfit - agg.totalFees + fundingFee,
		PnLRate:            pnlRate,
	}, nil
}

func (c *Client) fetchTradesAndFunding(ctx context.Context, symbol string, orderInfo *bitmartOrderInfo) ([]bitmartTradeRow, float64, error) {
	trades, err := c.rawGetTrades(ctx, symbol, orderInfo)
	if err != nil {
		return nil, 0, err
	}
	fundingFee, err := c.getFundingFee(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		return nil, 0, fmt.Errorf("bitmart get funding fee failed: %w", err)
	}
	return trades, fundingFee, nil
}

func (c *Client) aggregateOpenCloseTrades(
	orderID string,
	closeSide int,
	orderCreateTime int64,
	trades []bitmartTradeRow,
) aggregatedTradeResults {
	var res aggregatedTradeResults
	for i := range trades {
		t := &trades[i]
		tVol := decmath.ParseFloat(t.Vol)
		tPrice := decmath.ParseFloat(t.Price)

		if t.OrderID == orderID {
			res.openVolSum += tVol
			res.openPriceVolSum += tPrice * tVol
			res.totalFees += decmath.ParseFloat(t.PaidFees)
		} else if t.Side == closeSide && t.CreateTime >= orderCreateTime {
			res.closeTradesFound = true
			res.closeVolSum += tVol
			res.closePriceVolSum += tPrice * tVol
			res.totalRealizedProfit += decmath.ParseFloat(t.RealizedProfit)
			res.totalFees += decmath.ParseFloat(t.PaidFees)
			if t.CreateTime > res.latestTradeTime {
				res.latestTradeTime = t.CreateTime
			}
		}
	}
	return res
}

func (c *Client) getFundingFee(ctx context.Context, symbol string, startTimeMs int64) (float64, error) {
	transactions, err := c.rawGetTransactions(ctx, symbol, startTimeMs)
	if err != nil {
		return 0, err
	}
	var totalFunding float64
	for _, row := range transactions {
		totalFunding += decmath.ParseFloat(row.Amount)
	}
	return totalFunding, nil
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
		HoldVolContract: vol,
		RawHoldVol:      vol,
		PositionType:    pType,
		OpenAvgPrice:    avgPrice,
		HoldAvgPrice:    avgPrice,
		CloseProfitLoss: pnl,
		Leverage:        levVal,
	}
}
