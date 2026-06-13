package mexc

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/samber/lo"
)

// Explicit request/response structs for account endpoints.

type mexcAssetInfo struct {
	Currency         string  `json:"currency"`
	PositionMargin   float64 `json:"positionMargin"`
	FrozenBalance    float64 `json:"frozenBalance"`
	AvailableBalance float64 `json:"availableBalance"`
	CashBalance      float64 `json:"cashBalance"`
	Equity           float64 `json:"equity"`
	Unrealized       float64 `json:"unrealized"`
	Bonus            float64 `json:"bonus"`
}

type mexcAssetRequest struct {
	Currency string `json:"currency,omitempty"`
}

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

// Private raw methods invoking the MEXC API.

func (c *Client) getRawAssets(ctx context.Context) ([]mexcAssetInfo, error) {
	body, err := c.GetCtx(ctx, "/api/v1/private/account/assets", nil)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcAssetInfo](body, "assets")
}

func (c *Client) getRawAssetByCurrency(ctx context.Context, req mexcAssetRequest) (*mexcAssetInfo, error) {
	path := fmt.Sprintf("/api/v1/private/account/asset/%s", req.Currency)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponse[mexcAssetInfo](body, "asset_by_currency")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, req mexcPositionsRequest) ([]mexcPosition, error) {
	params := map[string]any{}
	if req.Symbol != "" {
		params[paramSymbol] = req.Symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/position/open_positions", params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[[]mexcPosition](body, "open_positions")
}

func (c *Client) getRawHistoryPositions(ctx context.Context, req mexcHistoryPositionsRequest) ([]mexcHistoryPosRow, error) {
	histParams := map[string]any{
		pageNumKey:  req.PageNum,
		pageSizeKey: req.PageSize,
	}
	if req.Symbol != "" {
		histParams["symbol"] = req.Symbol
	}
	if req.StartTime > 0 {
		histParams["start_time"] = req.StartTime
	}
	histBody, err := c.GetCtx(ctx, "/api/v1/private/position/list/history_positions", histParams)
	if err != nil {
		return nil, err
	}

	var histResp struct {
		Success bool                `json:"success"`
		Code    int                 `json:"code"`
		Message string              `json:"message"`
		Data    []mexcHistoryPosRow `json:"data"`
	}
	if err := json.Unmarshal(histBody, &histResp); err != nil {
		return nil, fmt.Errorf("unmarshal history positions: %w", err)
	}
	if !histResp.Success {
		return nil, fmt.Errorf("history positions api error [%d]: %s", histResp.Code, histResp.Message)
	}
	return histResp.Data, nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	rawList, err := c.getRawAssets(ctx)
	if err != nil {
		return nil, err
	}

	assets := make([]exchange.AssetInfo, len(rawList))
	for i, raw := range rawList {
		assets[i] = exchange.AssetInfo{
			Currency:         raw.Currency,
			PositionMargin:   raw.PositionMargin,
			FrozenBalance:    raw.FrozenBalance,
			AvailableBalance: raw.AvailableBalance,
			CashBalance:      raw.CashBalance,
			Equity:           raw.Equity,
			Unrealized:       raw.Unrealized,
			Bonus:            raw.Bonus,
		}
	}
	return assets, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	raw, err := c.getRawAssetByCurrency(ctx, mexcAssetRequest{Currency: currency})
	if err != nil {
		return nil, err
	}

	return &exchange.AssetInfo{
		Currency:         raw.Currency,
		PositionMargin:   raw.PositionMargin,
		FrozenBalance:    raw.FrozenBalance,
		AvailableBalance: raw.AvailableBalance,
		CashBalance:      raw.CashBalance,
		Equity:           raw.Equity,
		Unrealized:       raw.Unrealized,
		Bonus:            raw.Bonus,
	}, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawPos, err := c.getRawOpenPositions(ctx, mexcPositionsRequest{Symbol: symbol})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, len(rawPos))
	for i := range rawPos {
		positions[i] = rawPos[i].toPosition()
	}
	return positions, nil
}

// GetRecentClosedPnL queries the historical closed position metrics from MEXC.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	if extOrderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}

	orderInfo, err := c.getRawOrderByExOrderID(ctx, mexcGetOrderByExternalRequest{Symbol: symbol, ExternalOID: extOrderID})
	if err != nil {
		return nil, fmt.Errorf("mexc get order by external ID %s details failed: %w", extOrderID, err)
	}
	positionID := orderInfo.PositionID
	if positionID == 0 {
		return nil, fmt.Errorf("no positionId found associated with external order %s", extOrderID)
	}

	req := mexcHistoryPositionsRequest{
		Symbol:   symbol,
		PageNum:  1,
		PageSize: 10,
	}
	if !startTime.IsZero() {
		req.StartTime = startTime.UnixMilli()
	}

	histData, err := c.getRawHistoryPositions(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	row, found := lo.Find(histData, func(item mexcHistoryPosRow) bool {
		return item.PositionID == positionID
	})
	if !found {
		return nil, fmt.Errorf("query closed pnl failed: position record for ID %d not yet closed/finalized in history", positionID)
	}

	timeDiff := time.Now().UnixMilli() - row.UpdateTime
	if timeDiff >= 15000 {
		return nil, fmt.Errorf("query closed pnl failed: found stale closed position record for ID %d (age: %s)", positionID, time.Duration(timeDiff)*time.Millisecond)
	}

	netPnl := row.CloseProfitLoss - row.TotalFee + row.HoldFee
	pnlRate := row.ProfitRatio * 100

	duration := max(row.UpdateTime-row.CreateTime, 0)

	return &exchange.ClosedPnLInfo{
		Exchange:   "mexc",
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
