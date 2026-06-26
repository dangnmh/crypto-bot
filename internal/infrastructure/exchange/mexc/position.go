package mexc

import (
	"context"
	"fmt"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"

	"github.com/samber/lo"

	"crypto-bot/pkg/xjson"
)

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

type mexcCloseAllPositionsRequest struct {
	Symbol string `json:"symbol"`
}

// Private raw methods invoking the MEXC API.

func (c *Client) rawGetOpenPositions(ctx context.Context, req mexcPositionsRequest) ([]mexcPosition, error) {
	params := map[string]string{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetOpenPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcPosition](body, "open_positions")
}

func (c *Client) rawGetHistoryPositions(ctx context.Context, req mexcHistoryPositionsRequest) ([]mexcHistoryPosRow, error) {
	histParams := map[string]string{
		pageNumKey:  fmt.Sprintf("%d", req.PageNum),
		pageSizeKey: fmt.Sprintf("%d", req.PageSize),
	}
	if req.Symbol != "" {
		histParams["symbol"] = req.Symbol
	}
	if req.StartTime > 0 {
		histParams["start_time"] = fmt.Sprintf("%d", req.StartTime)
	}
	histBody, err := c.GetHistoryPositionsRaw(ctx, histParams)
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

func (c *Client) rawCloseAllPositions(ctx context.Context, req mexcCloseAllPositionsRequest) error {
	body, err := c.PostCtx(ctx, "/api/v1/private/position/close_all", req)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "close_all_positions")
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

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

// CloseAllPositions closes all positions for a symbol.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	return c.rawCloseAllPositions(ctx, mexcCloseAllPositionsRequest{Symbol: symbol})
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
		req.StartTime = orderInfo.CreateTime - 1000 // 1 second in milliseconds
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
		Exchange:   exchangeName,
		Symbol:     row.Symbol,
		EntryPrice: row.OpenAvgPrice,
		ExitPrice:  row.CloseAvgPrice,
		ClosedSize: row.CloseVol,
		GrossPnL:   row.CloseProfitLoss,
		Fee:        row.TotalFee,
		FundingFee: row.HoldFee,
		DurationMs: duration,
		NetPnl:     netPnl,
		PnLRate:    pnlRate,
	}, nil
}
