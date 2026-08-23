package futures

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/kucoin"
	"crypto-bot/pkg/decmath"
)

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

func (c *Client) rawGetOpenPositions(ctx context.Context, _ kucoinOpenPositionsRequest) ([]kucoinPosition, error) {
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/positions", nil, nil, true)
	if err != nil {
		return nil, err
	}
	return kucoin.ParseResponse[[]kucoinPosition](body, "open_positions")
}

func (c *Client) rawGetFills(ctx context.Context, orderID string) ([]kucoinFillItem, error) {
	params := map[string]string{
		"orderId": orderID,
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/fills", params, nil, true)
	if err != nil {
		return nil, err
	}
	res, err := kucoin.ParseResponse[kucoinFillsData](body, "get_fills")
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func (c *Client) rawGetPositionsHistory(ctx context.Context, symbol string, startTime time.Time) ([]kucoinPositionHistoryItem, error) {
	params := make(map[string]string)
	if symbol != "" {
		params["symbol"] = symbol
	}
	if !startTime.IsZero() {
		params["from"] = strconv.FormatInt(startTime.UnixMilli(), 10)
	}
	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/history-positions", params, nil, true)
	if err != nil {
		return nil, err
	}
	res, err := kucoin.ParseResponse[kucoinPositionHistoryData](body, "get_positions_history")
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// GetOpenPositions retrieves currently active futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	positions, err := c.rawGetOpenPositions(ctx, kucoinOpenPositionsRequest{})
	if err != nil {
		return nil, err
	}

	openPositions := make([]exchange.Position, 0, len(positions))
	for i := range positions {
		p := &positions[i]
		if symbol != "" && p.Symbol != symbol {
			continue
		}

		if p.CurrentQty == 0 {
			continue
		}

		openPositions = append(openPositions, p.toPosition())
	}

	return openPositions, nil
}

// ClosePosition is a helper to close a position.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	submitSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		submitSide = exchange.SideCloseShort
	}

	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Side:         submitSide,
		Type:         exchange.OrderTypeMarket,
		Vol:          volume,
		PositionMode: positionMode,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "kucoin"),
		Leverage:     leverage,
	})
	return err
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		pos := &positions[i]
		closeSide := domain.SideCloseLong
		if pos.PositionType == exchange.PositionTypeShort {
			closeSide = domain.SideCloseShort
		}
		vol := pos.HoldVolContract
		if vol == 0 {
			vol = pos.HoldVolCoin
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, vol, domain.PositionModeHedge, pos.Leverage)
	}

	return nil
}

// GetOrderPNL queries historical position records, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("kucoin get order by ID %s failed: %w", orderID, err)
	}

	if orderInfo.State == exchange.OrderStateCanceled && orderInfo.DealVol == 0 {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
			Status:   orderInfo.State,
		}, nil
	}

	fills, err := c.rawGetFills(ctx, orderInfo.OrderID)
	if err != nil {
		return nil, fmt.Errorf("kucoin get fills for closing order %s failed: %w", orderInfo.OrderID, err)
	}
	if len(fills) == 0 {
		return nil, nil
	}

	var closedSize float64
	for i := range fills {
		closedSize += float64(fills[i].Size)
	}

	var startTime time.Time
	if orderInfo.CreateTime > 0 {
		startTime = time.UnixMilli(orderInfo.CreateTime - 1000)
	}

	histPositions, err := c.rawGetPositionsHistory(ctx, symbol, startTime)
	if err != nil {
		return nil, fmt.Errorf("kucoin get history positions failed: %w", err)
	}

	var matchedItem *kucoinPositionHistoryItem
	for i := range histPositions {
		if histPositions[i].Symbol == symbol {
			matchedItem = &histPositions[i]
			break
		}
	}

	if matchedItem == nil {
		return nil, nil
	}

	entryPrice := decmath.ParseFloat(matchedItem.OpenPrice)
	exitPrice := decmath.ParseFloat(matchedItem.ClosePrice)
	netPnL := decmath.ParseFloat(matchedItem.Pnl)
	tradeFee := decmath.ParseFloat(matchedItem.TradeFee)
	fundingFee := decmath.ParseFloat(matchedItem.FundingFee)

	grossPnL := netPnL + tradeFee - fundingFee
	duration := max(matchedItem.CloseTime-matchedItem.OpenTime, 0)

	return &exchange.ClosedPnLInfo{
		Exchange:           exchangeName,
		Symbol:             matchedItem.Symbol,
		Status:             orderInfo.State,
		EntryPrice:         entryPrice,
		ExitPrice:          exitPrice,
		ClosedSizeContract: &closedSize,
		GrossPnL:           grossPnL,
		Fee:                tradeFee,
		FundingFee:         fundingFee,
		DurationMs:         duration,
		NetPnl:             netPnL,
	}, nil
}
