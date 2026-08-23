package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/samber/lo"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
	"crypto-bot/pkg/xjson"
)

const exchangeName = "mexc"

type mexcPositionsRequest struct {
	Symbol string `json:"symbol,omitempty"`
}

type mexcHistoryPosRow struct {
	PositionID      int64   `json:"positionId"`
	Symbol          string  `json:"symbol"`
	PositionType    int     `json:"positionType"`
	OpenType        int     `json:"openType"`
	State           int     `json:"state"`
	HoldVol         float64 `json:"holdVol"`
	FrozenVol       float64 `json:"frozenVol"`
	CloseVol        float64 `json:"closeVol"`
	HoldAvgPrice    float64 `json:"holdAvgPrice"`
	OpenAvgPrice    float64 `json:"openAvgPrice"`
	CloseAvgPrice   float64 `json:"closeAvgPrice"`
	OIM             float64 `json:"oim"`
	IM              float64 `json:"im"`
	HoldFee         float64 `json:"holdFee"`
	Realised        float64 `json:"realized"`
	Leverage        int     `json:"leverage"`
	CreateTime      int64   `json:"createTime"`
	UpdateTime      int64   `json:"updateTime"`
	CloseProfitLoss float64 `json:"closeProfitLoss"`
	Fee             float64 `json:"fee"`
	TotalFee        float64 `json:"totalFee"`
	ProfitRatio     float64 `json:"profitRatio"`
}

type mexcHistoryPositionsRequest struct {
	Symbol    string `json:"symbol,omitempty"`
	StartTime int64  `json:"start_time,omitempty"`
	PageNum   int    `json:"page_num"`
	PageSize  int    `json:"page_size"`
}

func (c *Client) rawGetOpenPositions(ctx context.Context, req mexcPositionsRequest) ([]mexcPosition, error) {
	params := map[string]any{}
	if req.Symbol != "" {
		params["symbol"] = req.Symbol
	}

	body, err := c.base.Request(ctx, http.MethodGet, "/api/v1/private/position/open_positions", params, nil, true)
	if err != nil {
		return nil, err
	}
	res, err := mexc.ParseFuturesResponse[[]mexcPosition](body)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

func (c *Client) rawGetHistoryPositions(ctx context.Context, req mexcHistoryPositionsRequest) ([]mexcHistoryPosRow, error) {
	histParams := map[string]any{
		"page_num":  fmt.Sprintf("%d", req.PageNum),
		"page_size": fmt.Sprintf("%d", req.PageSize),
	}
	if req.Symbol != "" {
		histParams["symbol"] = req.Symbol
	}
	if req.StartTime > 0 {
		histParams["start_time"] = fmt.Sprintf("%d", req.StartTime)
	}
	histBody, err := c.base.Request(ctx, http.MethodGet, "/api/v1/private/position/list/history_positions", histParams, nil, true)
	if err != nil {
		return nil, err
	}

	var histResp struct {
		Success bool                `json:"success"`
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Data    []mexcHistoryPosRow `json:"data"`
	}
	if err := xjson.Unmarshal(histBody, &histResp); err != nil {
		return nil, fmt.Errorf("unmarshal history positions: %w", err)
	}
	if !histResp.Success {
		return nil, fmt.Errorf("history positions api error [%d]: %s", histResp.Code, histResp.Message)
	}
	return histResp.Data, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawPos, err := c.rawGetOpenPositions(ctx, mexcPositionsRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, len(rawPos))
	for i := range rawPos {
		positions[i] = rawPos[i].toPosition()
	}
	return positions, nil
}

// CloseAllPositions closes all open positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	req := map[string]string{"symbol": symbol}
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return err
	}
	body, err := c.base.Request(ctx, http.MethodPost, "/api/v1/private/position/close_all", nil, bodyBytes, true)
	if err != nil {
		return err
	}
	_, err = mexc.ParseFuturesResponse[json.RawMessage](body)
	return err
}

// ClosePosition closes one position leg using a reduce-only market order.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	req := mexcCreateOrderRequest{
		Symbol:       symbol,
		Vol:          volume,
		Side:         int(closeSide),
		Type:         int(exchange.OrderTypeMarket),
		PositionMode: int(positionMode),
		ReduceOnly:   true,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "mexc"),
		Leverage:     leverage,
	}
	_, err := c.rawCreateOrder(ctx, req)
	return err
}

// GetOrderPNL queries the historical closed position metrics from MEXC.
func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	if orderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	orderInfo, err := c.rawGetOrder(ctx, mexcGetOrderRequest{OrderID: orderID})
	if err != nil {
		return nil, fmt.Errorf("mexc get order by ID %s details failed: %w", orderID, err)
	}
	mappedOrder := orderInfo.toOrderInfo()

	if mappedOrder != nil && mappedOrder.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
			Status:   mappedOrder.State,
		}, nil
	}
	positionID := orderInfo.PositionID
	if positionID == 0 {
		return nil, fmt.Errorf("no positionId found associated with order %s", orderID)
	}

	req := mexcHistoryPositionsRequest{
		Symbol:   symbol,
		PageNum:  1,
		PageSize: 10,
	}
	if orderInfo.CreateTime > 0 {
		req.StartTime = orderInfo.CreateTime - 1000
	}

	histData, err := c.rawGetHistoryPositions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	row, found := lo.Find(histData, func(item mexcHistoryPosRow) bool {
		return item.PositionID == positionID
	})
	if !found {
		return nil, fmt.Errorf("query closed pnl failed: position record for ID %d not yet closed/finalized in history", positionID)
	}

	netPnl := row.CloseProfitLoss - row.TotalFee + row.HoldFee
	pnlRate := row.ProfitRatio * 100
	duration := max(row.UpdateTime-row.CreateTime, 0)

	return &exchange.ClosedPnLInfo{
		Exchange:           exchangeName,
		Symbol:             row.Symbol,
		Status:             mappedOrder.State,
		EntryPrice:         row.OpenAvgPrice,
		ExitPrice:          row.CloseAvgPrice,
		ClosedSizeContract: &row.CloseVol,
		GrossPnL:           row.CloseProfitLoss,
		Fee:                row.TotalFee,
		FundingFee:         row.HoldFee,
		DurationMs:         duration,
		NetPnl:             netPnl,
		PnLRate:            pnlRate,
	}, nil
}
