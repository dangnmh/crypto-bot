package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

const exchangeName = "bingx"

// Explicit request/response structs for account endpoints.

type bingxUserIncomeRequest struct {
	Symbol     string `json:"symbol,omitempty"`
	IncomeType string `json:"incomeType,omitempty"`
	StartTime  int64  `json:"startTime,omitempty"`
	EndTime    int64  `json:"endTime,omitempty"`
	Limit      int    `json:"limit,omitempty"`
}

type bingxIncomeRow struct {
	Symbol     string `json:"symbol"`
	IncomeType string `json:"incomeType"`
	Income     string `json:"income"`
	Time       int64  `json:"time"`
	ID         string `json:"id"`
}

type bingxWalletBalanceRequest struct{}

type bingxPositionsRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type bingxBalance struct {
	Asset           string `json:"asset"`
	Balance         string `json:"balance"`
	Equity          string `json:"equity"`
	AvailableMargin string `json:"availableMargin"`
}

type bingxPosition struct {
	Symbol           string `json:"symbol"`
	PositionSide     string `json:"positionSide"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	UnrealizedProfit string `json:"unrealizedProfit"`
	Leverage         string `json:"leverage"`
	Isolated         bool   `json:"isolated"`
}

// Private raw methods invoking the BingX REST API.

func (c *Client) getRawAssets(ctx context.Context, _ bingxWalletBalanceRequest) ([]bingxBalance, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	type balanceData struct {
		Balance bingxBalance `json:"balance"`
	}

	var res []bingxBalance
	if err := json.Unmarshal(body, &res); err != nil {
		parsed, err := ParseResponse[[]bingxBalance](body, "get_assets")
		if err == nil {
			res = parsed
		} else {
			single, err := ParseResponse[balanceData](body, "get_assets")
			if err == nil {
				res = []bingxBalance{single.Balance}
			} else {
				return nil, fmt.Errorf("parse assets response: %w", err)
			}
		}
	}
	return res, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, req bingxPositionsRequest) ([]bingxPosition, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bingxPosition](body, "open_positions")
}

func (c *Client) getRawUserIncome(ctx context.Context, req bingxUserIncomeRequest) ([]bingxIncomeRow, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}
	if req.IncomeType != "" {
		params["incomeType"] = req.IncomeType
	}
	if req.StartTime > 0 {
		params["startTime"] = strconv.FormatInt(req.StartTime, 10)
	}
	if req.EndTime > 0 {
		params["endTime"] = strconv.FormatInt(req.EndTime, 10)
	}
	if req.Limit > 0 {
		params[paramLimit] = strconv.Itoa(req.Limit)
	}

	body, err := c.GetCtx(ctx, pathUserIncome, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bingxIncomeRow](body, "user_income")
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets fetches all active margin balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	res, err := c.getRawAssets(ctx, bingxWalletBalanceRequest{})
	if err != nil {
		return nil, err
	}

	assets := make([]exchange.AssetInfo, 0, len(res))
	for i := range res {
		b := &res[i]
		bal := decmath.ParseFloat(b.Balance)
		eq := decmath.ParseFloat(b.Equity)
		avail := decmath.ParseFloat(b.AvailableMargin)

		assets = append(assets, exchange.AssetInfo{
			Currency:         b.Asset,
			Equity:           eq,
			AvailableBalance: avail,
			CashBalance:      bal,
		})
	}

	return assets, nil
}

// GetAssetByCurrency retrieves margin balance for a specific coin.
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

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions retrieves currently active futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	res, err := c.getRawOpenPositions(ctx, bingxPositionsRequest{
		Symbol: symbol,
	})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(res))
	for i := range res {
		p := &res[i]

		amt := decmath.ParseFloat(p.PositionAmt)
		if amt == 0 {
			continue
		}

		sideVal := exchange.PositionTypeLong // long.
		if p.PositionSide == posSideShort || (p.PositionSide == posSideBoth && amt < 0) {
			sideVal = exchange.PositionTypeShort // short.
		}

		absAmt := math.Abs(amt)
		entry := decmath.ParseFloat(p.EntryPrice)

		positions = append(positions, exchange.Position{
			Symbol:       p.Symbol,
			HoldVol:      absAmt,
			HoldAvgPrice: entry,
			OpenAvgPrice: entry,
			PositionType: sideVal,
		})
	}

	return positions, nil
}

// GetRecentClosedPnL queries recent trades from BingX, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up the opening order details.
	orderInfo, err := c.GetOrderByExternalID(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("bingx get order by external ID %s failed: %w", extOrderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	if orderInfo.DealVol == 0 {
		return nil, fmt.Errorf("zero deal volume for opening order %s", extOrderID)
	}

	incomeEntries, err := c.fetchUserIncome(ctx, symbol, startTime)
	if err != nil {
		return nil, err
	}

	agg := c.aggregateUserIncome(incomeEntries)

	entryPrice := orderInfo.DealAvgPrice
	if entryPrice == 0 {
		entryPrice = orderInfo.Price
	}
	closedSize := orderInfo.DealVol

	isOpenLong := orderInfo.Side == exchange.SideOpenLong
	var exitPrice float64
	if isOpenLong {
		exitPrice = entryPrice + (agg.grossPnl / closedSize)
	} else {
		exitPrice = entryPrice - (agg.grossPnl / closedSize)
	}

	durationMs := int64(0)
	if agg.latestTime > 0 {
		durationMs = max(agg.latestTime-startTime.UnixMilli(), 0)
	}

	return &exchange.ClosedPnLInfo{
		Exchange:   exchangeName,
		Symbol:     symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   agg.grossPnl,
		Fee:        agg.fee,
		FundingFee: agg.fundingFee,
		DurationMs: durationMs,
		NetPnl:     agg.grossPnl - agg.fee + agg.fundingFee,
	}, nil
}

func (c *Client) fetchUserIncome(ctx context.Context, symbol string, startTime time.Time) ([]bingxIncomeRow, error) {
	req := bingxUserIncomeRequest{
		Symbol:    symbol,
		StartTime: startTime.UnixMilli(),
		Limit:     100,
	}
	incomeEntries, err := c.getRawUserIncome(ctx, req)
	if err != nil {
		return nil, err
	}

	// Ensure we have at least one REALIZED_PNL entry
	hasRealizedPnl := false
	for _, row := range incomeEntries {
		if row.IncomeType == incomeTypeRealizedPnL {
			hasRealizedPnl = true
			break
		}
	}

	if !hasRealizedPnl {
		return nil, fmt.Errorf("closing realized PnL trade not found yet (propagation delay)")
	}

	return incomeEntries, nil
}

type aggregatedIncome struct {
	grossPnl   float64
	fee        float64
	fundingFee float64
	latestTime int64
}

func (c *Client) aggregateUserIncome(entries []bingxIncomeRow) aggregatedIncome {
	var agg aggregatedIncome
	for _, row := range entries {
		val := decmath.ParseFloat(row.Income)
		switch row.IncomeType {
		case incomeTypeRealizedPnL:
			agg.grossPnl += val
			if row.Time > agg.latestTime {
				agg.latestTime = row.Time
			}
		case incomeTypeTradingFee:
			agg.fee += math.Abs(val)
		case incomeTypeFundingFee:
			agg.fundingFee += val
		}
	}
	return agg
}

// CreateListenKey starts a new user data stream and returns its listenKey.
func (c *Client) CreateListenKey(ctx context.Context) (string, error) {
	body, err := c.PostCtx(ctx, "/openApi/user/auth/userDataStream", nil, nil)
	if err != nil {
		return "", err
	}

	type listenKeyData struct {
		ListenKey string `json:"listenKey"`
	}
	resp, err := ParseResponse[listenKeyData](body, "create_listen_key")
	if err != nil {
		return "", err
	}
	return resp.ListenKey, nil
}

// KeepAliveListenKey pings the active user data stream to keep it open.
func (c *Client) KeepAliveListenKey(ctx context.Context, listenKey string) error {
	params := map[string]string{
		"listenKey": listenKey,
	}
	body, err := c.PutCtx(ctx, "/openApi/user/auth/userDataStream", params, nil)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "keepalive_listen_key")
}
