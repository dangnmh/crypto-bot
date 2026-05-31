package binance

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures/src/restapi/models"
	"github.com/cenkalti/backoff/v4"
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
	orderInfo, err := c.GetOrder(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("binance get order by external ID %s failed: %w", extOrderID, err)
	}
	openingOrderId, parseErr := strconv.ParseInt(orderInfo.OrderID, 10, 64)
	if parseErr != nil {
		return nil, fmt.Errorf("binance parse numeric order ID %s failed: %w", orderInfo.OrderID, parseErr)
	}

	// TODO: Symbol-based trade queries can be subject to collisions if multiple bots trade the same symbol concurrently.
	// To make this 100% precise, we should track the exact closingOrderId returned by the position close call
	// and query userTrades specifically by openingOrderId and closingOrderId instead of relying on symbol & startTime.
	req := c.sdkClient.RestApi.TradeAPI.AccountTradeList(ctx).
		Symbol(symbol).
		Limit(50)

	if !startTime.IsZero() {
		req = req.StartTime(startTime.UnixMilli())
	}

	var items []models.AccountTradeListResponseInner

	operation := func() error {
		resp, err := c.sdkClient.RestApi.TradeAPI.AccountTradeListExecute(req)
		if err != nil {
			return err
		}
		if len(resp.Data.Items) == 0 {
			return fmt.Errorf("no user trades found for symbol %s since %v", symbol, startTime)
		}
		items = resp.Data.Items
		return nil
	}

	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(time.Millisecond*200),
				backoff.WithMaxInterval(time.Second*2)),
			4),
		ctx,
	)

	if err := backoff.RetryNotify(operation, bo, func(err error, d time.Duration) {
		c.logger.ErrorContext(ctx, "retry closed trades query", slog.String("symbol", symbol), slog.String("error", err.Error()), slog.Duration("delay", d))
	}); err != nil {
		return nil, fmt.Errorf("query closed trades failed: %w", err)
	}

	var openingQty float64
	var openingWeightedPriceSum float64
	var openingCommission float64

	var closingQty float64
	var closingWeightedPriceSum float64
	var closingCommission float64
	var totalRealizedPnl float64

	var latestTrade models.AccountTradeListResponseInner
	var hasClosingTrade bool

	for i := range items {
		item := &items[i]
		qty := decmath.ParseFloat(item.GetQty())
		price := decmath.ParseFloat(item.GetPrice())
		realizedPnl := decmath.ParseFloat(item.GetRealizedPnl())
		commission := decmath.ParseFloat(item.GetCommission())
		itemOrderId := item.GetOrderId()

		if itemOrderId == openingOrderId {
			openingQty += qty
			openingWeightedPriceSum += price * qty
			openingCommission += commission
		} else {
			closingQty += qty
			closingWeightedPriceSum += price * qty
			closingCommission += commission
			totalRealizedPnl += realizedPnl
			latestTrade = *item
			hasClosingTrade = true
		}
	}

	if openingQty == 0 {
		return nil, fmt.Errorf("zero opening quantity for order %d", openingOrderId)
	}

	entryPrice := openingWeightedPriceSum / openingQty
	var exitPrice float64
	var closedSize float64
	var fee float64

	if hasClosingTrade && closingQty > 0 {
		exitPrice = closingWeightedPriceSum / closingQty
		closedSize = closingQty
		fee = openingCommission + closingCommission
	} else {
		// Fallback if closing trades are not yet in the ledger (asynchronous propagation delay)
		// We use entry price as exit price fallback
		exitPrice = entryPrice
		closedSize = openingQty
		fee = openingCommission
	}

	durationMs := int64(0)
	if hasClosingTrade && !startTime.IsZero() {
		durationMs = max(latestTrade.GetTime()-startTime.UnixMilli(), 0)
	}

	fdFee, err := c.getHoldFee(ctx, symbol, startTime)
	if err != nil {
		c.logger.Debug("Binance failed to query income history for funding fee", slog.Any("error", err))
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   totalRealizedPnl,
		Fee:        fee,
		FundingFee: fdFee,
		DurationMs: durationMs,
		NetPnl:     totalRealizedPnl - fee + fdFee,
	}, nil
}

func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime time.Time) (float64, error) {
	if startTime.IsZero() {
		return 0, nil
	}

	req := c.sdkClient.RestApi.AccountAPI.GetIncomeHistory(ctx).
		Symbol(symbol).
		IncomeType("FUNDING_FEE").
		StartTime(startTime.UnixMilli()).
		Limit(10)

	resp, err := c.sdkClient.RestApi.AccountAPI.GetIncomeHistoryExecute(req)
	if err != nil {
		return 0, err
	}

	totalHoldFee := 0.0
	for _, item := range resp.Data.Items {
		if item.Income != nil {
			fee := decmath.ParseFloat(*item.Income)
			totalHoldFee += fee
		}
	}

	return totalHoldFee, nil
}
