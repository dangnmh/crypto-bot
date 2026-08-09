package binance

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

const exchangeName = "binance"

// Explicit request/response structs for account/position endpoints.

type binancePositionsRequest struct {
	Symbol     string
	RecvWindow *int64
}

type binanceAccountTradeListRequest struct {
	Symbol    string
	Limit     int32
	StartTime int64
}

type binanceIncomeHistoryRequest struct {
	Symbol     string
	IncomeType string
	StartTime  int64
	Limit      int32
}

// Private raw methods invoking the Binance API directly.

func (c *Client) rawGetOpenPositions(ctx context.Context, req binancePositionsRequest) ([]positionRiskItem, error) {
	params := make(map[string]any)
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.RecvWindow != nil {
		params["recvWindow"] = *req.RecvWindow
	}

	var resp []positionRiskItem
	err := c.request(ctx, http.MethodGet, "/fapi/v3/positionRisk", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance position information: %w", err)
	}
	return resp, nil
}

func (c *Client) rawGetAccountTrades(ctx context.Context, req binanceAccountTradeListRequest) ([]userTradeItem, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	if req.Limit > 0 {
		params["limit"] = req.Limit
	}
	if req.StartTime > 0 {
		params["startTime"] = req.StartTime
	}

	var resp []userTradeItem
	err := c.request(ctx, http.MethodGet, "/fapi/v1/userTrades", params, true, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) rawGetIncomeHistory(ctx context.Context, req binanceIncomeHistoryRequest) ([]incomeHistoryItem, error) {
	params := make(map[string]any)
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}
	if req.IncomeType != "" {
		params["incomeType"] = req.IncomeType
	}
	if req.StartTime > 0 {
		params["startTime"] = req.StartTime
	}
	if req.Limit > 0 {
		params["limit"] = req.Limit
	}

	var resp []incomeHistoryItem
	err := c.request(ctx, http.MethodGet, "/fapi/v1/income", params, true, &resp)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetOpenPositions returns all open positions for a symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	resp, err := c.rawGetOpenPositions(ctx, binancePositionsRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(resp))

	for i := range resp {
		raw := &resp[i]
		amt := decmath.ParseFloat(raw.PositionAmt)

		if math.Abs(amt) == 0 {
			continue
		}

		entryPrice := decmath.ParseFloat(raw.EntryPrice)

		posType := exchange.PositionTypeLong
		if amt < 0 {
			posType = exchange.PositionTypeShort
		}

		if raw.PositionSide == posSideShort {
			posType = exchange.PositionTypeShort
		}

		lev, _ := strconv.Atoi(raw.Leverage)

		positions = append(positions, exchange.Position{
			Symbol:       raw.Symbol,
			HoldVolCoin:  math.Abs(amt),
			RawHoldVol:   math.Abs(amt),
			HoldAvgPrice: entryPrice,
			OpenAvgPrice: entryPrice,
			PositionType: posType,
			Leverage:     lev,
		})
	}

	return positions, nil
}

// ClosePosition closes a single position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         closeSide,
		Type:         exchange.OrderTypeMarket,
		PositionMode: positionMode,
		ReduceOnly:   true,
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "binance"),
	}
	_, err := c.CreateOrder(ctx, req)
	return err
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := positions[i]
		vol := pos.HoldVolCoin
		if vol == 0 {
			vol = pos.HoldVolContract
		}
		if vol > 0 {
			side := domain.SideCloseShort
			if pos.PositionType == exchange.PositionTypeLong {
				side = domain.SideCloseLong
			}
			err = c.ClosePosition(ctx, symbol, side, vol, domain.PositionModeHedge, pos.Leverage)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

type aggregatedTradeResults struct {
	openingQty              float64
	openingWeightedPriceSum float64
	openingCommission       float64
	closingQty              float64
	closingWeightedPriceSum float64
	closingCommission       float64
	totalRealizedPnl        float64
	latestTrade             userTradeItem
	hasClosingTrade         bool
}

// GetOrderPNL queries recent trades from Binance, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("binance get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}
	openingOrderId, parseErr := strconv.ParseInt(orderInfo.OrderID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("binance parse numeric order ID %s failed: %w", orderInfo.OrderID, parseErr)
	}

	var startTime time.Time
	if orderInfo.CreateTime > 0 {
		startTime = time.UnixMilli(orderInfo.CreateTime - 1000)
	}

	req := binanceAccountTradeListRequest{
		Symbol: symbol,
		Limit:  50,
	}

	if !startTime.IsZero() {
		req.StartTime = startTime.UnixMilli()
	}

	isOpenLong := (orderInfo.Side == exchange.SideOpenLong)

	items, err := c.rawGetAccountTrades(ctx, req)
	if err != nil {
		return nil, err
	}

	agg := c.aggregateClosedTrades(items, openingOrderId, startTime, isOpenLong)

	if agg.openingQty == 0 {
		return nil, fmt.Errorf("zero opening quantity for order %d", openingOrderId)
	}

	entryPrice, exitPrice, closedSize, fee := computeClosedPnLMetrics(agg, isOpenLong)

	durationMs := int64(0)
	if agg.hasClosingTrade && !startTime.IsZero() {
		durationMs = max(agg.latestTrade.Time-startTime.UnixMilli(), 0)
	}

	fdFee, err := c.getHoldFee(ctx, symbol, startTime)
	if err != nil {
		c.logger.Debug("Binance failed to query income history for funding fee", slog.Any("error", err))
	}

	return &exchange.ClosedPnLInfo{
		Exchange:       exchangeName,
		Symbol:         symbol,
		EntryPrice:     entryPrice,
		ExitPrice:      exitPrice,
		ClosedSizeCoin: new(closedSize),
		GrossPnL:       agg.totalRealizedPnl,
		Fee:            fee,
		FundingFee:     fdFee,
		DurationMs:     durationMs,
		NetPnl:         agg.totalRealizedPnl - fee + fdFee,
	}, nil
}

func computeClosedPnLMetrics(
	agg aggregatedTradeResults,
	isOpenLong bool,
) (float64, float64, float64, float64) {
	var entryPrice float64
	var exitPrice float64
	var closedSize float64
	var fee float64

	if agg.hasClosingTrade && agg.closingQty > 0 {
		exitPrice = agg.closingWeightedPriceSum / agg.closingQty
		closedSize = agg.closingQty
		fee = agg.openingCommission + agg.closingCommission
		if isOpenLong {
			entryPrice = (agg.closingWeightedPriceSum - agg.totalRealizedPnl) / agg.closingQty
		} else {
			entryPrice = (agg.closingWeightedPriceSum + agg.totalRealizedPnl) / agg.closingQty
		}
	} else {
		entryPrice = agg.openingWeightedPriceSum / agg.openingQty
		exitPrice = entryPrice
		closedSize = agg.openingQty
		fee = agg.openingCommission
	}
	return entryPrice, exitPrice, closedSize, fee
}

func (c *Client) aggregateClosedTrades(
	items []userTradeItem,
	openingOrderId int64,
	startTime time.Time,
	isOpenLong bool,
) aggregatedTradeResults {
	var res aggregatedTradeResults

	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	for i := range items {
		item := &items[i]
		qty := decmath.ParseFloat(item.Qty)
		price := decmath.ParseFloat(item.Price)
		realizedPnl := decmath.ParseFloat(item.RealizedPnl)
		commission := decmath.ParseFloat(item.Commission)
		itemOrderId := item.OrderId

		if itemOrderId == openingOrderId {
			res.openingQty += qty
			res.openingWeightedPriceSum += price * qty
			res.openingCommission += commission
		} else {
			if !startTime.IsZero() && item.Time < startTime.UnixMilli() {
				continue
			}

			isOppositeSide := false
			if isOpenLong {
				isOppositeSide = !item.Buyer || item.Side == sideSell
			} else {
				isOppositeSide = item.Buyer || item.Side == sideBuy
			}

			if !isOppositeSide {
				continue
			}

			res.closingQty += qty
			res.closingWeightedPriceSum += price * qty
			res.closingCommission += commission
			res.totalRealizedPnl += realizedPnl
			res.latestTrade = *item
			res.hasClosingTrade = true
		}
	}

	return res
}

func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime time.Time) (float64, error) {
	if startTime.IsZero() {
		return 0, nil
	}

	resp, err := c.rawGetIncomeHistory(ctx, binanceIncomeHistoryRequest{
		Symbol:     symbol,
		IncomeType: "FUNDING_FEE",
		StartTime:  startTime.UnixMilli(),
		Limit:      10,
	})
	if err != nil {
		return 0, err
	}

	totalHoldFee := 0.0
	for i := range resp {
		item := &resp[i]
		if item.Income != nil {
			fee := decmath.ParseFloat(*item.Income)
			totalHoldFee += fee
		}
	}

	return totalHoldFee, nil
}
