package toobit

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

const exchangeName = "toobit"

type toobitListenKey struct {
	ListenKey string `json:"listenKey"`
}

// CreateListenKey creates a new user stream listen key.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	body, err := c.request(ctx, http.MethodPost, "/api/v1/listenKey", nil, true)
	if err != nil {
		return "", err
	}
	var res toobitListenKey
	if err := xjson.Unmarshal(body, &res); err != nil {
		return "", fmt.Errorf("unmarshal listenKey: %w", err)
	}
	return res.ListenKey, nil
}

// KeepAliveListenKey keeps the active user stream listen key alive.
func (c *Client) KeepAliveListenKey(ctx context.Context, listenKey string) error {
	params := map[string]string{
		"listenKey": listenKey,
	}
	_, err := c.request(ctx, http.MethodPut, "/api/v1/listenKey", params, true)
	return err
}

// getHoldFee queries the futures balance flow log to find funding fee payments for a symbol.
func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime int64) (float64, error) {
	if startTime == 0 {
		return 0, nil
	}

	params := map[string]string{
		symbolKey:   symbol,
		"startTime": strconv.FormatInt(startTime, 10),
		"limit":     "1000",
	}

	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/balanceFlow", params, true)
	if err != nil {
		return 0, err
	}

	rows, err := parseResponse[[]toobitFuturesBalanceFlowRow](body)
	if err != nil {
		return 0, err
	}

	totalFunding := 0.0
	for _, row := range rows {
		if row.FlowTypeValue == 32 {
			val, _ := strconv.ParseFloat(row.Change, 64)
			totalFunding += val
		}
	}

	return totalFunding, nil
}

// GetOrderPNL queries the historical closed position metrics from Toobit.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("toobit get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == domain.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	var positionSide string
	if orderInfo.Side == exchange.SideOpenLong || orderInfo.Side == exchange.SideCloseLong {
		positionSide = posSideLong
	} else {
		positionSide = posSideShort
	}

	params := map[string]string{
		"symbol": symbol,
		"side":   positionSide,
	}

	body, err := c.GetHistoryPositionsRaw(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("query closed position history failed: %w", err)
	}

	histData, err := parseResponse[[]toobitHistoryPosition](body)
	if err != nil {
		return nil, fmt.Errorf("parse history positions: %w", err)
	}

	if len(histData) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no closed position history found for symbol %s", symbol)
	}

	row := histData[0]

	entryPrice, _ := strconv.ParseFloat(row.OpenAvgPrice, 64)
	exitPrice, _ := strconv.ParseFloat(row.CloseAvgPrice, 64)
	closedSize, _ := strconv.ParseFloat(row.CloseTotalQty, 64)
	grossPnL, _ := strconv.ParseFloat(row.RealizedPnlWithoutFee, 64)
	netPnL, _ := strconv.ParseFloat(row.RealizedPnL, 64)
	openFee, _ := strconv.ParseFloat(row.OpenFee, 64)
	closeFee, _ := strconv.ParseFloat(row.CloseFee, 64)

	duration := max(xjson.ToInt64(row.CloseTime)-xjson.ToInt64(row.OpenTime), 0)

	pnlRate := 0.0
	if entryPrice > 0 {
		if positionSide == posSideLong {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		} else {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	fundingFee, err := c.getHoldFee(ctx, symbol, xjson.ToInt64(row.OpenTime))
	if err != nil {
		c.logger.Debug("failed to fetch funding fee", "error", err)
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        math.Abs(openFee) + math.Abs(closeFee),
		FundingFee: fundingFee,
		DurationMs: duration,
		NetPnl:     netPnL,
		PnLRate:    pnlRate,
	}, nil
}
