package bitget

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
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
	InstID           string `json:"instId"`
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

type bitgetAssetsRequest struct {
	ProductType string `json:"productType"`
}

type bitgetOpenPositionsRequest struct {
	ProductType string `json:"productType"`
}

type bitgetHistoryPositionsRequest struct {
	Symbol      string `json:"symbol,omitempty"`
	ProductType string `json:"productType"`
	StartTime   string `json:"startTime,omitempty"`
	EndTime     string `json:"endTime,omitempty"`
	Limit       string `json:"limit,omitempty"`
	IDLessThan  string `json:"idLessThan,omitempty"`
}

type bitgetHistoryPosition struct {
	PositionID    string `json:"positionId"`
	MarginCoin    string `json:"marginCoin"`
	Symbol        string `json:"symbol"`
	HoldSide      string `json:"holdSide"`
	OpenAvgPrice  string `json:"openAvgPrice"`
	CloseAvgPrice string `json:"closeAvgPrice"`
	OpenTotalPos  string `json:"openTotalPos"`
	CloseTotalPos string `json:"closeTotalPos"`
	PnL           string `json:"pnl"`
	NetProfit     string `json:"netProfit"`
	TotalFunding  string `json:"totalFunding"`
	OpenFee       string `json:"openFee"`
	CloseFee      string `json:"closeFee"`
	CTime         string `json:"ctime"`
	UTime         string `json:"utime"`
}

type bitgetHistoryPositionResponse struct {
	List  []bitgetHistoryPosition `json:"list"`
	EndID string                  `json:"endId"`
}

// Private raw methods invoking the Bitget REST API.

func (c *Client) getRawAssets(ctx context.Context, req bitgetAssetsRequest) ([]bitgetAccountAsset, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}

	body, err := c.GetCtx(ctx, pathAccountBalance, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bitgetAccountAsset](body, "assets")
}

func (c *Client) getRawOpenPositions(ctx context.Context, req bitgetOpenPositionsRequest) ([]bitgetPosition, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}

	body, err := c.GetCtx(ctx, pathOpenPositions, params)
	if err != nil {
		return nil, err
	}

	return ParseResponse[[]bitgetPosition](body, "open_positions")
}

func (c *Client) getRawHistoryPositions(ctx context.Context, req bitgetHistoryPositionsRequest) (*bitgetHistoryPositionResponse, error) {
	params := map[string]string{
		paramProductType: req.ProductType,
	}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}
	if req.StartTime != "" {
		params["startTime"] = req.StartTime
	}
	if req.EndTime != "" {
		params["endTime"] = req.EndTime
	}
	if req.Limit != "" {
		params[paramLimit] = req.Limit
	}
	if req.IDLessThan != "" {
		params["idLessThan"] = req.IDLessThan
	}

	body, err := c.GetCtx(ctx, pathHistoryPositions, params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[bitgetHistoryPositionResponse](body, "history_position")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public mapper methods implementing the exchange.AccountDataProvider interface.

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	balances, err := c.getRawAssets(ctx, bitgetAssetsRequest{
		ProductType: productTypeUsdtFutures,
	})
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

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	positions, err := c.getRawOpenPositions(ctx, bitgetOpenPositionsRequest{
		ProductType: productTypeUsdtFutures,
	})
	if err != nil {
		return nil, err
	}

	var openPositions []exchange.Position
	for i := range positions {
		pos := &positions[i]
		posSym := pos.Symbol
		if posSym == "" {
			posSym = pos.InstID
		}
		if symbol != "" && posSym != symbol {
			continue
		}

		holdVol, _ := strconv.ParseFloat(pos.Total, 64)
		if holdVol <= 0 {
			continue
		}

		avgPx, _ := strconv.ParseFloat(pos.OpenPriceAvg, 64)

		posType := exchange.PositionTypeLong // Long.
		if pos.HoldSide == posSideShort {
			posType = exchange.PositionTypeShort
		}

		openPositions = append(openPositions, exchange.Position{
			Symbol:       posSym,
			HoldVol:      holdVol,
			HoldAvgPrice: avgPx,
			OpenAvgPrice: avgPx,
			PositionType: posType,
		})
	}

	return openPositions, nil
}

// GetRecentClosedPnL queries the recent trade fills from Bitget for a symbol, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	// Look up numeric orderID from client order ID (extOrderID / clientOid) to get update time.
	orderInfo, err := c.GetOrder(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("bitget get order by external ID %s failed: %w", extOrderID, err)
	}
	orderTime := orderInfo.UpdateTime

	req := bitgetHistoryPositionsRequest{
		ProductType: productTypeUsdtFutures,
		Symbol:      symbol,
		Limit:       "100",
	}
	if !startTime.IsZero() {
		req.StartTime = strconv.FormatInt(startTime.UnixMilli(), 10)
	}

	res, err := c.getRawHistoryPositions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	if res == nil || len(res.List) == 0 {
		return nil, fmt.Errorf("query closed pnl failed: no history position records found for symbol %s", symbol)
	}

	var matchedPos *bitgetHistoryPosition
	for i := range res.List {
		p := &res.List[i]
		pCloseTime := decmath.ParseInt64(p.UTime)
		// Check if the record close time matches the order update time within a 10s tolerance window
		if math.Abs(float64(pCloseTime-orderTime)) <= 10000 {
			matchedPos = p
			break
		}
	}

	if matchedPos == nil {
		return nil, fmt.Errorf("query closed pnl failed: no matching history position record found for order update time %d", orderTime)
	}

	entryPrice := decmath.ParseFloat(matchedPos.OpenAvgPrice)
	exitPrice := decmath.ParseFloat(matchedPos.CloseAvgPrice)
	closedSize := decmath.ParseFloat(matchedPos.CloseTotalPos)
	grossPnL := decmath.ParseFloat(matchedPos.PnL)
	netPnl := decmath.ParseFloat(matchedPos.NetProfit)
	fundingFee := decmath.ParseFloat(matchedPos.TotalFunding)
	openFee := decmath.ParseFloat(matchedPos.OpenFee)
	closeFee := decmath.ParseFloat(matchedPos.CloseFee)

	ctime := decmath.ParseInt64(matchedPos.CTime)
	utime := decmath.ParseInt64(matchedPos.UTime)
	duration := max(utime-ctime, 0)

	pnlRate := 0.0
	if entryPrice > 0 {
		switch matchedPos.HoldSide {
		case posSideLong:
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		case posSideShort:
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     matchedPos.Symbol,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSize,
		GrossPnL:   grossPnL,
		Fee:        openFee + closeFee,
		FundingFee: fundingFee,
		DurationMs: duration,
		NetPnl:     netPnl,
		PnLRate:    pnlRate,
	}, nil
}
