package binance

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"

	"github.com/binance/binance-connector-go/clients/derivativestradingusdsfutures/src/restapi/models"
	"github.com/cenkalti/backoff/v4"
	"github.com/samber/lo"
)

// Explicit request/response structs for account endpoints.

type binanceWalletBalanceRequest struct{}

type binancePositionsRequest struct {
	Symbol string
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

// Private raw methods invoking the Binance SDK.

func (c *Client) getRawAssets(ctx context.Context, _ binanceWalletBalanceRequest) (*models.FuturesAccountBalanceV2Response, error) {
	req := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2(ctx)
	resp, err := c.sdkClient.RestApi.AccountAPI.FuturesAccountBalanceV2Execute(req)
	if err != nil {
		return nil, fmt.Errorf("binance futures account balance: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, req binancePositionsRequest) (*models.PositionInformationV3Response, error) {
	r := c.sdkClient.RestApi.TradeAPI.PositionInformationV3(ctx)
	if req.Symbol != "" {
		r = r.Symbol(req.Symbol)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.PositionInformationV3Execute(r)
	if err != nil {
		return nil, fmt.Errorf("binance position information: %w", err)
	}
	return &resp.Data, nil
}

func (c *Client) getRawAccountTrades(ctx context.Context, req binanceAccountTradeListRequest) (*models.AccountTradeListResponse, error) {
	r := c.sdkClient.RestApi.TradeAPI.AccountTradeList(ctx).
		Symbol(req.Symbol)
	if req.Limit > 0 {
		r = r.Limit(int64(req.Limit))
	}
	if req.StartTime > 0 {
		r = r.StartTime(req.StartTime)
	}

	resp, err := c.sdkClient.RestApi.TradeAPI.AccountTradeListExecute(r)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

func (c *Client) getRawIncomeHistory(ctx context.Context, req binanceIncomeHistoryRequest) (*models.GetIncomeHistoryResponse, error) {
	r := c.sdkClient.RestApi.AccountAPI.GetIncomeHistory(ctx).
		Symbol(req.Symbol).
		IncomeType(req.IncomeType)
	if req.StartTime > 0 {
		r = r.StartTime(req.StartTime)
	}
	if req.Limit > 0 {
		r = r.Limit(int64(req.Limit))
	}

	resp, err := c.sdkClient.RestApi.AccountAPI.GetIncomeHistoryExecute(r)
	if err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets returns all account assets and balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	resp, err := c.getRawAssets(ctx, binanceWalletBalanceRequest{})
	if err != nil {
		return nil, err
	}

	items := resp.Items
	assets := make([]exchange.AssetInfo, 0, len(items))

	for i := range items {
		item := &items[i]
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
	resp, err := c.getRawOpenPositions(ctx, binancePositionsRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	items := resp.Items
	positions := make([]exchange.Position, 0, len(items))

	for i := range items {
		raw := &items[i]
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

type aggregatedTradeResults struct {
	openingQty              float64
	openingWeightedPriceSum float64
	openingCommission       float64
	closingQty              float64
	closingWeightedPriceSum float64
	closingCommission       float64
	totalRealizedPnl        float64
	latestTrade             models.AccountTradeListResponseInner
	hasClosingTrade         bool
}

// GetRecentClosedPnL queries recent trades from Binance, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up numeric orderID from client order ID (extOrderID / clientOid).
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
	req := binanceAccountTradeListRequest{
		Symbol: symbol,
		Limit:  50,
	}

	if !startTime.IsZero() {
		req.StartTime = startTime.UnixMilli()
	}

	isOpenLong := (orderInfo.Side == exchange.SideOpenLong)

	items, _, err := c.fetchAccountTradesWithRetry(ctx, req, symbol, openingOrderId, startTime, isOpenLong, orderInfo.DealVol)
	if err != nil {
		return nil, err
	}

	agg := c.aggregateClosedTrades(items, openingOrderId, startTime, isOpenLong, orderInfo.DealVol)

	if agg.openingQty == 0 {
		return nil, fmt.Errorf("zero opening quantity for order %d", openingOrderId)
	}

	entryPrice := agg.openingWeightedPriceSum / agg.openingQty
	var exitPrice float64
	var closedSize float64
	var fee float64

	if agg.hasClosingTrade && agg.closingQty > 0 {
		exitPrice = agg.closingWeightedPriceSum / agg.closingQty
		closedSize = agg.closingQty
		fee = agg.openingCommission + agg.closingCommission
	} else {
		// Fallback if closing trades are not yet in the ledger (asynchronous propagation delay).
		// We use entry price as exit price fallback.
		exitPrice = entryPrice
		closedSize = agg.openingQty
		fee = agg.openingCommission
	}

	durationMs := int64(0)
	if agg.hasClosingTrade && !startTime.IsZero() {
		durationMs = max(agg.latestTrade.GetTime()-startTime.UnixMilli(), 0)
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
		GrossPnL:   agg.totalRealizedPnl,
		Fee:        fee,
		FundingFee: fdFee,
		DurationMs: durationMs,
		NetPnl:     agg.totalRealizedPnl - fee + fdFee,
	}, nil
}

func (c *Client) aggregateClosedTrades(
	items []models.AccountTradeListResponseInner,
	openingOrderId int64,
	startTime time.Time,
	isOpenLong bool,
	expectedVol float64,
) aggregatedTradeResults {
	var res aggregatedTradeResults

	// Sort trades by Trade ID (strictly sequential and ascending) to ensure chronological order for capping
	sort.Slice(items, func(i, j int) bool {
		return items[i].GetId() < items[j].GetId()
	})

	remainingCloseVol := expectedVol

	for i := range items {
		item := &items[i]
		qty := decmath.ParseFloat(item.GetQty())
		price := decmath.ParseFloat(item.GetPrice())
		realizedPnl := decmath.ParseFloat(item.GetRealizedPnl())
		commission := decmath.ParseFloat(item.GetCommission())
		itemOrderId := item.GetOrderId()

		if itemOrderId == openingOrderId {
			res.openingQty += qty
			res.openingWeightedPriceSum += price * qty
			res.openingCommission += commission
		} else {
			// Time filter: must be >= startTime
			if !startTime.IsZero() && item.GetTime() < startTime.UnixMilli() {
				continue
			}

			// Side Filter: must be opposite side of opening order
			isOppositeSide := false
			if isOpenLong {
				isOppositeSide = !item.GetBuyer() || item.GetSide() == sideSell
			} else {
				isOppositeSide = item.GetBuyer() || item.GetSide() == sideBuy
			}

			if !isOppositeSide {
				continue
			}

			if expectedVol > 0 && remainingCloseVol <= 0 {
				continue // Skip extra closing trades once target volume is reached
			}

			takeQty := qty
			ratio := 1.0
			if expectedVol > 0 && qty > remainingCloseVol {
				takeQty = remainingCloseVol
				ratio = remainingCloseVol / qty
				remainingCloseVol = 0
			} else if expectedVol > 0 {
				remainingCloseVol -= qty
			}

			res.closingQty += takeQty
			res.closingWeightedPriceSum += price * takeQty
			res.closingCommission += commission * ratio
			res.totalRealizedPnl += realizedPnl * ratio
			res.latestTrade = *item
			res.hasClosingTrade = true
		}
	}

	return res
}

func (c *Client) fetchAccountTradesWithRetry(
	ctx context.Context,
	req binanceAccountTradeListRequest,
	symbol string,
	openingOrderId int64,
	startTime time.Time,
	isOpenLong bool,
	expectedVol float64,
) ([]models.AccountTradeListResponseInner, bool, error) {
	var items []models.AccountTradeListResponseInner
	var hasClosing bool

	operation := func() error {
		var err error
		items, hasClosing, err = c.tryFetchAccountTrades(ctx, req, openingOrderId, startTime, isOpenLong, expectedVol)
		if err != nil {
			return err
		}
		if !hasClosing {
			return fmt.Errorf("closing trade not found yet (propagation delay)")
		}
		return nil
	}

	bo := backoff.WithContext(
		backoff.WithMaxRetries(backoff.NewExponentialBackOff(
			backoff.WithInitialInterval(time.Second),
			backoff.WithMaxInterval(time.Second*2)),
			4),
		ctx,
	)

	err := backoff.RetryNotify(operation, bo, func(err error, d time.Duration) {
		c.logger.InfoContext(ctx, "retry closed trades query", slog.String("symbol", symbol), slog.String("error", err.Error()), slog.Duration("delay", d))
	})

	if err != nil {
		if len(items) > 0 {
			return items, false, nil
		}
		return nil, false, err
	}

	return items, hasClosing, nil
}

func (c *Client) tryFetchAccountTrades(
	ctx context.Context,
	req binanceAccountTradeListRequest,
	openingOrderId int64,
	startTime time.Time,
	isOpenLong bool,
	expectedVol float64,
) ([]models.AccountTradeListResponseInner, bool, error) {
	resp, err := c.getRawAccountTrades(ctx, req)
	if err != nil {
		return nil, false, err
	}
	if len(resp.Items) == 0 {
		return nil, false, fmt.Errorf("no user trades found")
	}

	var closingQty float64
	for i := range resp.Items {
		item := &resp.Items[i]
		if item.GetOrderId() != openingOrderId {
			// Time Filter: must be >= startTime
			if !startTime.IsZero() && item.GetTime() < startTime.UnixMilli() {
				continue
			}
			// Side Filter: must be opposite side of opening order
			isOppositeSide := false
			if isOpenLong {
				isOppositeSide = !item.GetBuyer() || item.GetSide() == sideSell
			} else {
				isOppositeSide = item.GetBuyer() || item.GetSide() == sideBuy
			}

			if isOppositeSide {
				closingQty += decmath.ParseFloat(item.GetQty())
			}
		}
	}

	hasClosing := false
	if expectedVol > 0 {
		if closingQty >= expectedVol-1e-9 {
			hasClosing = true
		}
	} else if closingQty > 0 {
		hasClosing = true
	}

	return resp.Items, hasClosing, nil
}

func (c *Client) getHoldFee(ctx context.Context, symbol string, startTime time.Time) (float64, error) {
	if startTime.IsZero() {
		return 0, nil
	}

	resp, err := c.getRawIncomeHistory(ctx, binanceIncomeHistoryRequest{
		Symbol:     symbol,
		IncomeType: "FUNDING_FEE",
		StartTime:  startTime.UnixMilli(),
		Limit:      10,
	})
	if err != nil {
		return 0, err
	}

	totalHoldFee := 0.0
	items := resp.Items
	for i := range items {
		item := &items[i]
		if item.Income != nil {
			fee := decmath.ParseFloat(*item.Income)
			totalHoldFee += fee
		}
	}

	return totalHoldFee, nil
}
