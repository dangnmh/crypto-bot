package mexc

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/cenkalti/backoff/v4"
)

// GetAssets returns all account asset information.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	body, err := c.GetCtx(ctx, "/api/v1/private/account/assets", nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]exchange.AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse assets response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get assets failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// GetAssetByCurrency returns asset info for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	path := fmt.Sprintf("/api/v1/private/account/asset/%s", currency)
	body, err := c.GetCtx(ctx, path, nil)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[exchange.AssetInfo]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse asset response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get asset failed [%d]: %s", resp.Code, resp.Message)
	}
	return &resp.Data, nil
}

func (c *Client) getRawOpenPositions(ctx context.Context, symbol string) ([]mexcPosition, error) {
	params := map[string]any{}
	if symbol != "" {
		params[paramSymbol] = symbol
	}

	body, err := c.GetCtx(ctx, "/api/v1/private/position/open_positions", params)
	if err != nil {
		return nil, err
	}

	var resp APIResponse[[]mexcPosition]
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("parse positions response: %w", err)
	}
	if !resp.Success {
		return nil, fmt.Errorf("get positions failed [%d]: %s", resp.Code, resp.Message)
	}
	return resp.Data, nil
}

// GetOpenPositions returns all open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawPos, err := c.getRawOpenPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, len(rawPos))
	for i := range rawPos {
		positions[i] = rawPos[i].toPosition()
	}
	return positions, nil
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

func (c *Client) getRawHistoryPositions(ctx context.Context, symbol string, startTime time.Time) ([]mexcHistoryPosRow, error) {
	histParams := map[string]any{
		pageNumKey:  1,
		pageSizeKey: 10,
	}
	if symbol != "" {
		histParams["symbol"] = symbol
	}
	if !startTime.IsZero() {
		histParams["start_time"] = startTime.UnixMilli()
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

func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	if extOrderID == "" {
		return nil, fmt.Errorf("orderID is required")
	}
	// Query order details by client order ID (externalOid) on MEXC
	orderInfo, err := c.getRawOrderByExOrderID(ctx, symbol, extOrderID)
	if err != nil {
		return nil, fmt.Errorf("mexc get order by external ID %s details failed: %w", extOrderID, err)
	}
	positionID := orderInfo.PositionID
	if positionID == 0 {
		return nil, fmt.Errorf("no positionId found associated with external order %s", extOrderID)
	}

	var row mexcHistoryPosRow

	// Step 2: Use exponential backoff to query history_positions for matching positionID
	operation := func() error {
		histData, err := c.getRawHistoryPositions(ctx, symbol, startTime)
		if err != nil {
			if strings.Contains(err.Error(), "unmarshal") {
				return backoff.Permanent(err)
			}
			return err
		}

		// Find the matching position by ID
		for i := range histData {
			p := &histData[i]
			if p.PositionID == positionID {
				// Check if the record is fresh (updated within the last 15 seconds)
				timeDiff := time.Now().UnixMilli() - p.UpdateTime
				if timeDiff >= 15000 {
					return fmt.Errorf("found stale closed position record for ID %d (age: %s)", positionID, time.Duration(timeDiff)*time.Millisecond)
				}
				row = *p
				return nil
			}
		}

		return fmt.Errorf("position record for ID %d not yet closed/finalized in history", positionID)
	}

	// Retry up to 5 times (4 retries + 1st try) with 200ms initial interval, up to 2s max interval
	bo := backoff.WithContext(
		backoff.WithMaxRetries(
			backoff.NewExponentialBackOff(
				backoff.WithInitialInterval(time.Millisecond*200),
				backoff.WithMaxInterval(time.Second*2)),
			4),
		ctx,
	)

	if err := backoff.RetryNotify(operation, bo, func(err error, d time.Duration) {
		c.logger.ErrorContext(ctx, "retry closed pnl query", slog.String("symbol", symbol), slog.String("error", err.Error()), slog.Duration("delay", d))
	}); err != nil {
		return nil, fmt.Errorf("query closed pnl failed: %w", err)
	}

	// Step 3: Calculate NetPnl and PnLRate
	netPnl := row.CloseProfitLoss - row.TotalFee + row.HoldFee
	pnlRate := row.ProfitRatio * 100

	duration := max(row.UpdateTime-row.CreateTime, 0)

	return &exchange.ClosedPnLInfo{
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
