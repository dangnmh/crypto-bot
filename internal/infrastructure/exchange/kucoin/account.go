package kucoin

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

const exchangeName = "kucoin"

type kucoinAccountOverview struct {
	Currency         string `json:"currency"`
	AccountEquity    string `json:"accountEquity"`
	AvailableBalance string `json:"availableBalance"`
	PositionMargin   string `json:"positionMargin"`
	OrderMargin      string `json:"orderMargin"`
	UnrealisedPNL    string `json:"unrealisedPNL"`
}

type kucoinPosition struct {
	Symbol           string      `json:"symbol"`
	CurrentQty       json.Number `json:"currentQty"`
	AvgEntryPrice    json.Number `json:"avgEntryPrice"`
	RealisedPNL      json.Number `json:"realisedPNL"`
	UnrealisedPNL    json.Number `json:"unrealisedPNL"`
	Leverage         json.Number `json:"leverage"`
	LiquidationPrice json.Number `json:"liquidationPrice"`
}

type kucoinAssetsRequest struct{}

type kucoinOpenPositionsRequest struct{}

type kucoinPositionHistoryItem struct {
	CloseID      string `json:"closeId"`
	Symbol       string `json:"symbol"`
	Leverage     string `json:"leverage"`
	Type         string `json:"type"`
	Pnl          string `json:"pnl"`
	TradeFee     string `json:"tradeFee"`
	FundingFee   string `json:"fundingFee"`
	OpenTime     int64  `json:"openTime"`
	CloseTime    int64  `json:"closeTime"`
	OpenPrice    string `json:"openPrice"`
	ClosePrice   string `json:"closePrice"`
	MarginMode   string `json:"marginMode"`
	PositionSide string `json:"positionSide"`
	Side         string `json:"side"`
}

type kucoinPositionHistoryData struct {
	Items []kucoinPositionHistoryItem `json:"items"`
}

type kucoinFillItem struct {
	TradeID   string `json:"tradeId"`
	OrderID   string `json:"orderId"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	Price     string `json:"price"`
	Size      int64  `json:"size"`
	Fee       string `json:"fee"`
	CreatedAt int64  `json:"createdAt"`
}

type kucoinFillsData struct {
	Items []kucoinFillItem `json:"items"`
}

// Private raw methods invoking the KuCoin REST API.

func (c *Client) getRawAssets(ctx context.Context, _ kucoinAssetsRequest) (*kucoinAccountOverview, error) {
	body, err := c.GetCtx(ctx, pathAccountBalance, nil)
	if err != nil {
		return nil, err
	}

	overview, err := ParseResponse[kucoinAccountOverview](body, "account_balance")
	if err != nil {
		return nil, err
	}
	return &overview, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, _ kucoinOpenPositionsRequest) ([]kucoinPosition, error) {
	body, err := c.GetCtx(ctx, pathOpenPositions, nil)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]kucoinPosition](body, "open_positions")
}

func (c *Client) getRawOrderByClientOid(ctx context.Context, clientOid string) (*kucoinOrder, error) {
	params := map[string]string{
		constantClientOid: clientOid,
	}
	body, err := c.GetCtx(ctx, pathGetOrderByClientOid, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinOrder](body, "get_order_by_client_oid")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawFills(ctx context.Context, orderID string) ([]kucoinFillItem, error) {
	params := map[string]string{
		"orderId": orderID,
	}
	body, err := c.GetCtx(ctx, pathFills, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinFillsData](body, "get_fills")
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *Client) getRawPositionsHistory(ctx context.Context, symbol string, startTime time.Time) ([]kucoinPositionHistoryItem, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}
	if !startTime.IsZero() {
		params["from"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	}
	body, err := c.GetCtx(ctx, pathPositionsHistory, params)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[kucoinPositionHistoryData](body, "get_positions_history")
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets fetches account balance overview.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	overview, err := c.getRawAssets(ctx, kucoinAssetsRequest{})
	if err != nil {
		return nil, err
	}

	eq := decmath.ParseFloat(overview.AccountEquity)
	avail := decmath.ParseFloat(overview.AvailableBalance)
	upl := decmath.ParseFloat(overview.UnrealisedPNL)

	return []exchange.AssetInfo{
		{
			Currency:         overview.Currency,
			Equity:           eq,
			AvailableBalance: avail,
			CashBalance:      eq,
			Unrealized:       upl,
		},
	}, nil
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
	positions, err := c.getRawOpenPositions(ctx, kucoinOpenPositionsRequest{})
	if err != nil {
		return nil, err
	}

	openPositions := make([]exchange.Position, 0, len(positions))
	for i := range positions {
		p := &positions[i]
		if symbol != "" && p.Symbol != symbol {
			continue
		}

		amt := decmath.ParseFloat(p.CurrentQty.String())
		if amt == 0 {
			continue
		}

		posType := exchange.PositionTypeLong // Long.
		if amt < 0 {
			posType = exchange.PositionTypeShort // Short.
		}

		absAmt := math.Abs(amt)
		avgPx := decmath.ParseFloat(p.AvgEntryPrice.String())

		openPositions = append(openPositions, exchange.Position{
			Symbol:       p.Symbol,
			HoldVol:      absAmt,
			HoldAvgPrice: avgPx,
			OpenAvgPrice: avgPx,
			PositionType: posType,
		})
	}

	return openPositions, nil
}

// GetRecentClosedPnL queries historical position records, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("kucoin get order by external ID %s failed: %w", extOrderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	// 1. Get closing order by client OID.
	closingOrder, err := c.getRawOrderByClientOid(ctx, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("kucoin get order by client OID %s failed: %w", extOrderID, err)
	}

	// 2. Query fills for that closing order to compute total closed size.
	fills, err := c.getRawFills(ctx, closingOrder.OrderID)
	if err != nil {
		return nil, fmt.Errorf("kucoin get fills for closing order %s failed: %w", closingOrder.OrderID, err)
	}

	var closedSize float64
	for i := range fills {
		sizeVal := float64(fills[i].Size)
		closedSize += sizeVal
	}

	// 3. Query positions history to match the closed position record.
	historyItems, err := c.getRawPositionsHistory(ctx, symbol, startTime)
	if err != nil {
		return nil, fmt.Errorf("query closed position history failed: %w", err)
	}
	if len(historyItems) == 0 {
		return nil, fmt.Errorf("query closed position history failed: no closed position history records found for symbol %s", symbol)
	}

	// Find the matching position record.
	var matchedItem *kucoinPositionHistoryItem
	for i := range historyItems {
		item := &historyItems[i]
		if item.Symbol == symbol {
			matchedItem = item
			break
		}
	}

	if matchedItem == nil {
		return nil, fmt.Errorf("no matching closed position record found for symbol %s", symbol)
	}

	entryPrice := decmath.ParseFloat(matchedItem.OpenPrice)
	exitPrice := decmath.ParseFloat(matchedItem.ClosePrice)
	netPnL := decmath.ParseFloat(matchedItem.Pnl)
	tradeFee := decmath.ParseFloat(matchedItem.TradeFee)
	fundingFee := decmath.ParseFloat(matchedItem.FundingFee)

	// Math reconstruction:
	// GrossPnL = NetPnL + tradeFee - fundingFee
	grossPnL := netPnL + tradeFee - fundingFee
	duration := max(matchedItem.CloseTime-matchedItem.OpenTime, 0)

	return &exchange.ClosedPnLInfo{
		Exchange:   "kucoin",
		Symbol:     matchedItem.Symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        tradeFee,
		FundingFee: fundingFee,
		DurationMs: duration,
		NetPnl:     netPnL,
	}, nil
}
