package binance

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/samber/lo"
)

// GetAssets returns all account assets and balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	req := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2(ctx)
	resp, err := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2Execute(req)
	if err != nil {
		return nil, fmt.Errorf("binance futures account balance: %w", err)
	}

	items := resp.Data.Items
	assets := make([]exchange.AssetInfo, 0, len(items))

	for _, item := range items {
		asset := item.GetAsset()
		balance := decmath.ParseFloat(lo.FromPtr(item.Balance))
		crossUnPnl := decmath.ParseFloat(lo.FromPtr(item.CrossUnPnl))
		available := decmath.ParseFloat(lo.FromPtr(item.AvailableBalance))

		assets = append(assets, exchange.AssetInfo{
			Currency:         asset,
			PositionMargin:   0.0,
			FrozenBalance:    0.0,
			AvailableBalance: available,
			CashBalance:      balance,
			Equity:           balance + crossUnPnl,
			Unrealized:       crossUnPnl,
		})
	}

	if len(assets) == 0 {
		assets = append(assets, exchange.AssetInfo{
			Currency: "USDT",
		})
	}

	return assets, nil
}

// GetAssetByCurrency queries asset information for a single currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for i := range assets {
		if strings.EqualFold(assets[i].Currency, currency) {
			return &assets[i], nil
		}
	}

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions returns all open positions for a symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	req := c.sdkClient.RestApi.TradeAPI.PositionInformationV2(ctx)
	if symbol != "" {
		req = req.Symbol(symbol)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.PositionInformationV2Execute(req)
	if err != nil {
		return nil, fmt.Errorf("binance position information: %w", err)
	}

	items := resp.Data.Items
	positions := make([]exchange.Position, 0, len(items))

	for i := range items {
		raw := items[i]
		amt := decmath.ParseFloat(lo.FromPtr(raw.PositionAmt))

		if math.Abs(amt) == 0 {
			continue
		}

		entryPrice := decmath.ParseFloat(lo.FromPtr(raw.EntryPrice))

		posType := 1
		if amt < 0 {
			posType = 2
		}

		if raw.GetPositionSide() == posSideShort {
			posType = 2
		}

		positions = append(positions, exchange.Position{
			Symbol:       raw.GetSymbol(),
			HoldVol:      math.Abs(amt),
			HoldAvgPrice: entryPrice,
			OpenAvgPrice: entryPrice,
			PositionType: posType,
		})
	}

	return positions, nil
}

// GetRecentClosedPnL queries recent trades from Binance, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up numeric orderID from client order ID (extOrderID / clientOid)
	orderInfo, err := c.GetOrder(ctx, symbol+":"+extOrderID)
	if err != nil {
		return nil, fmt.Errorf("binance get order by external ID %s failed: %w", extOrderID, err)
	}
	closingOrderId, parseErr := strconv.ParseInt(orderInfo.OrderID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("binance parse numeric order ID %s failed: %w", orderInfo.OrderID, parseErr)
	}

	req := c.sdkClient.RestApi.TradeAPI.AccountTradeList(ctx).
		Symbol(symbol).
		Limit(10)

	if !startTime.IsZero() {
		req = req.StartTime(startTime.UnixMilli())
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.AccountTradeListExecute(req)
	if err != nil {
		return nil, fmt.Errorf("binance get closed trades: %w", err)
	}

	items := resp.Data.Items
	if len(items) == 0 {
		return nil, fmt.Errorf("no user trades found for symbol %s", symbol)
	}

	// The list is returned in chronological order; the last item is the latest trade fill.
	latestTrade := items[len(items)-1]

	var totalQty float64
	var totalRealizedPnl float64
	var totalCommission float64
	var weightedPriceSum float64

	for i := range items {
		item := &items[i]
		if item.GetOrderId() == closingOrderId {
			qty := decmath.ParseFloat(item.GetQty())
			price := decmath.ParseFloat(item.GetPrice())
			realizedPnl := decmath.ParseFloat(item.GetRealizedPnl())
			commission := decmath.ParseFloat(item.GetCommission())

			totalQty += qty
			totalRealizedPnl += realizedPnl
			totalCommission += commission
			weightedPriceSum += price * qty
		}
	}

	if totalQty == 0 {
		return nil, fmt.Errorf("zero quantity for closing order %d", closingOrderId)
	}

	exitPrice := weightedPriceSum / totalQty
	var entryPrice float64

	// If the closing execution was SELL, the position was LONG.
	isLong := strings.EqualFold(latestTrade.GetSide(), "SELL")
	if isLong {
		entryPrice = exitPrice - (totalRealizedPnl / totalQty)
	} else {
		entryPrice = exitPrice + (totalRealizedPnl / totalQty)
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     latestTrade.GetSymbol(),
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: totalQty,
		GrossPnL:   totalRealizedPnl,
		Fee:        totalCommission,
		FundingFee: 0,
		DurationMs: 0,
	}, nil
}
