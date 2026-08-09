package bitget

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

const exchangeName = "bitget"

type bitgetPosition struct {
	Symbol           string       `json:"symbol"`
	InstID           string       `json:"instId"`
	HoldSide         string       `json:"holdSide"`
	MarginMode       string       `json:"marginMode"`
	Leverage         xjson.Number `json:"leverage"`
	Total            xjson.Number `json:"total"`
	Available        xjson.Number `json:"available"`
	Locked           xjson.Number `json:"locked"`
	OpenPriceAvg     xjson.Number `json:"openPriceAvg"`
	MarginSize       xjson.Number `json:"marginSize"`
	UnrealizedPL     xjson.Number `json:"unrealizedPL"`
	LiquidationPrice xjson.Number `json:"liquidationPrice"`
	AchievedProfits  xjson.Number `json:"achievedProfits"`
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
	PositionID    string       `json:"positionId"`
	MarginCoin    string       `json:"marginCoin"`
	Symbol        string       `json:"symbol"`
	HoldSide      string       `json:"holdSide"`
	OpenAvgPrice  xjson.Number `json:"openAvgPrice"`
	CloseAvgPrice xjson.Number `json:"closeAvgPrice"`
	OpenTotalPos  xjson.Number `json:"openTotalPos"`
	CloseTotalPos xjson.Number `json:"closeTotalPos"`
	PnL           xjson.Number `json:"pnl"`
	NetProfit     xjson.Number `json:"netProfit"`
	TotalFunding  xjson.Number `json:"totalFunding"`
	OpenFee       xjson.Number `json:"openFee"`
	CloseFee      xjson.Number `json:"closeFee"`
	CTime         xjson.Number `json:"ctime"`
	UTime         xjson.Number `json:"utime"`
}

type bitgetHistoryPositionResponse struct {
	List  []bitgetHistoryPosition `json:"list"`
	EndID string                  `json:"endId"`
}

// Private raw methods invoking the Bitget REST API.

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

		holdVol := xjson.ToFloat64(pos.Total)
		if holdVol <= 0 {
			continue
		}

		avgPx := xjson.ToFloat64(pos.OpenPriceAvg)

		posType := exchange.PositionTypeLong // Long.
		if pos.HoldSide == posSideShort {
			posType = exchange.PositionTypeShort
		}

		lev := int(xjson.ToInt64(pos.Leverage))

		openPositions = append(openPositions, exchange.Position{
			Symbol:          posSym,
			HoldVolContract: holdVol,
			RawHoldVol:      holdVol,
			HoldAvgPrice:    avgPx,
			OpenAvgPrice:    avgPx,
			PositionType:    posType,
			Leverage:        lev,
		})
	}

	return openPositions, nil
}

// GetOrderPNL queries the recent trade fills from Bitget for a symbol, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	// Look up order info by ID to get update time.
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("bitget get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	var startTime time.Time
	if orderInfo.CreateTime > 0 {
		startTime = time.UnixMilli(orderInfo.CreateTime - 1000)
	}

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

	matchedPos := &res.List[0]

	entryPrice := xjson.ToFloat64(matchedPos.OpenAvgPrice)
	exitPrice := xjson.ToFloat64(matchedPos.CloseAvgPrice)
	closedSize := xjson.ToFloat64(matchedPos.CloseTotalPos)
	grossPnL := xjson.ToFloat64(matchedPos.PnL)
	netPnl := xjson.ToFloat64(matchedPos.NetProfit)
	fundingFee := xjson.ToFloat64(matchedPos.TotalFunding)
	openFee := xjson.ToFloat64(matchedPos.OpenFee)
	closeFee := xjson.ToFloat64(matchedPos.CloseFee)

	ctime := xjson.ToInt64(matchedPos.CTime)
	utime := xjson.ToInt64(matchedPos.UTime)
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
		Exchange:           exchangeName,
		Symbol:             symbol,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosedSizeContract: new(closedSize),
		GrossPnL:           grossPnL,
		Fee:                openFee + closeFee,
		FundingFee:         fundingFee,
		DurationMs:         duration,
		NetPnl:             netPnl,
		PnLRate:            pnlRate,
	}, nil
}
