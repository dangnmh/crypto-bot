//nolint:misspell // API compatibility requires "realised" spelling
package bitmart

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

const exchangeName = "bitmart"

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

// getRawOrder retrieves detailed raw order information directly from BitMart.
func (c *Client) getRawOrder(ctx context.Context, symbol, orderID string) (*bitmartOrderInfo, error) {
	query := map[string]string{
		paramSymbol:  symbol,
		paramOrderID: orderID,
	}
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
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   agg.totalRealizedProfit,
		Fee:        agg.totalFees,
		FundingFee: fundingFee,
		DurationMs: duration,
		NetPnl:     agg.totalRealizedProfit - agg.totalFees + fundingFee,
		PnLRate:    pnlRate,
	}, nil
}

func (c *Client) fetchTradesAndFunding(ctx context.Context, symbol string, orderInfo *bitmartOrderInfo) ([]bitmartTradeRow, float64, error) {
	query := map[string]string{
		paramSymbol: symbol,
	}
	if orderInfo.CreateTime > 0 {
		startTimeSec := (orderInfo.CreateTime / 1000) - 60
		query["start_time"] = strconv.FormatInt(startTimeSec, 10)
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/trades", query, nil, true)
	if err != nil {
		return nil, 0, fmt.Errorf("bitmart get trades failed: %w", err)
	}
	var tradeResp bitmartTradeResponse
	if err := xjson.Unmarshal(body, &tradeResp); err != nil {
		return nil, 0, fmt.Errorf("unmarshal trades response: %w", err)
	}
	fundingFee, err := c.getFundingFee(ctx, symbol, orderInfo.CreateTime)
	if err != nil {
		return nil, 0, fmt.Errorf("bitmart get funding fee failed: %w", err)
	}
	return tradeResp.Data, fundingFee, nil
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
	query := map[string]string{
		paramSymbol:  symbol,
		"flow_type":  "3", // Funding Fee
		"start_time": strconv.FormatInt(startTimeMs, 10),
	}
	body, err := c.requestFull(ctx, http.MethodGet, "/contract/private/transaction-history", query, nil, true)
	if err != nil {
		return 0, err
	}
	var resp bitmartTransactionResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return 0, err
	}
	var totalFunding float64
	for _, row := range resp.Data {
		totalFunding += decmath.ParseFloat(row.Amount)
	}
	return totalFunding, nil
}
