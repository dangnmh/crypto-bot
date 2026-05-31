package bitget

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
)

type bitgetAccountAsset struct {
	MarginCoin    string `json:"marginCoin"`
	Locked        string `json:"locked"`
	Available     string `json:"available"`
	AccountEquity string `json:"accountEquity"`
	UnrealizedPL  string `json:"unrealizedPL"`
}

type bitgetPosition struct {
	Symbol           string `json:"symbol"`
	HoldSide         string `json:"holdSide"`
	MarginMode       string `json:"marginMode"`
	Leverage         string `json:"leverage"`
	Total            string `json:"total"`
	Available        string `json:"available"`
	Locked           string `json:"locked"`
	OpenPriceAvg     string `json:"openPriceAvg"`
	MarginSize       string `json:"marginSize"`
	UnrealizedPL     string `json:"unrealizedPL"`
	LiquidationPrice string `json:"liquidationPrice"`
	AchievedProfits  string `json:"achievedProfits"`
}

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathAccountBalance, params)
	if err != nil {
		return nil, err
	}

	balances, err := ParseResponse[[]bitgetAccountAsset](body, "assets")
	if err != nil {
		return nil, err
	}

	assets := make([]exchange.AssetInfo, 0, len(balances))
	for i := range balances {
		item := &balances[i]
		eq, _ := strconv.ParseFloat(item.AccountEquity, 64)
		avail, _ := strconv.ParseFloat(item.Available, 64)
		locked, _ := strconv.ParseFloat(item.Locked, 64)
		upl, _ := strconv.ParseFloat(item.UnrealizedPL, 64)

		assets = append(assets, exchange.AssetInfo{
			Currency:         item.MarginCoin,
			Equity:           eq,
			AvailableBalance: avail,
			FrozenBalance:    locked,
			CashBalance:      eq,
			Unrealized:       upl,
		})
	}

	return assets, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for i := range assets {
		if assets[i].Currency == currency {
			return &assets[i], nil
		}
	}

	return nil, fmt.Errorf("asset balance not found for currency: %s", currency)
}

func (c *Client) getRawOpenPositions(ctx context.Context) ([]bitgetPosition, error) {
	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bitgetPosition](body, "open_positions")
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	positions, err := c.getRawOpenPositions(ctx)
	if err != nil {
		return nil, err
	}

	var openPositions []exchange.Position
	for i := range positions {
		pos := &positions[i]
		if symbol != "" && pos.Symbol != symbol {
			continue
		}

		holdVol, _ := strconv.ParseFloat(pos.Total, 64)
		if holdVol <= 0 {
			continue
		}

		avgPx, _ := strconv.ParseFloat(pos.OpenPriceAvg, 64)

		posType := 1 // long
		if pos.HoldSide == posSideShort {
			posType = 2
		}

		openPositions = append(openPositions, exchange.Position{
			Symbol:       pos.Symbol,
			HoldVol:      holdVol,
			HoldAvgPrice: avgPx,
			OpenAvgPrice: avgPx,
			PositionType: posType,
		})
	}

	return openPositions, nil
}

type bitgetTradeFill struct {
	FillID  string `json:"fillId"`
	OrderID string `json:"orderId"`
	Symbol  string `json:"symbol"`
	Side    string `json:"side"`
	FillPx  string `json:"fillPx"`
	FillSz  string `json:"fillSz"`
	Fee     string `json:"fee"`
	CTime   string `json:"cTime"`
}

// GetRecentClosedPnL queries the recent trade fills from Bitget for a symbol, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up numeric orderID from client order ID (extOrderID / clientOid)
	orderInfo, err := c.GetOrder(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("bitget get order by external ID %s failed: %w", extOrderID, err)
	}
	closingOrderId := orderInfo.OrderID

	params := map[string]string{
		paramProductType: productTypeUsdtFutures,
		paramLimit:       "10",
	}
	if !startTime.IsZero() {
		params["startTime"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v2/mix/order/fills", params)
	if err != nil {
		return nil, err
	}

	fills, err := ParseResponse[[]bitgetTradeFill](body, "fills")
	if err != nil {
		return nil, err
	}

	if len(fills) == 0 {
		return nil, fmt.Errorf("no user fills found for symbol %s", symbol)
	}

	// Find the latest fill representing the latest closing execution
	latestFill := &fills[0]
	for i := range fills {
		f := &fills[i]
		cTimeF, _ := strconv.ParseInt(f.CTime, 10, 64)
		cTimeLatest, _ := strconv.ParseInt(latestFill.CTime, 10, 64)
		if cTimeF > cTimeLatest {
			latestFill = f
		}
	}

	var totalQty float64
	var totalCommission float64
	var weightedPriceSum float64

	for i := range fills {
		item := &fills[i]
		if item.OrderID != closingOrderId {
			continue
		}
		qty, _ := strconv.ParseFloat(item.FillSz, 64)
		price, _ := strconv.ParseFloat(item.FillPx, 64)
		commission, _ := strconv.ParseFloat(item.Fee, 64)

		totalQty += qty
		totalCommission += commission
		weightedPriceSum += price * qty
	}

	if totalQty == 0 {
		return nil, fmt.Errorf("zero quantity for closing order %s", closingOrderId)
	}

	exitPrice := weightedPriceSum / totalQty

	return &exchange.ClosedPnLInfo{
		Symbol:     latestFill.Symbol,
		EntryPrice: 0, // Fallback to WS estimation
		ExitPrice:  exitPrice,
		ClosedSize: totalQty,
		GrossPnL:   0, // Fallback to WS estimation
		Fee:        totalCommission,
		FundingFee: 0,
		DurationMs: 0,
	}, nil
}
