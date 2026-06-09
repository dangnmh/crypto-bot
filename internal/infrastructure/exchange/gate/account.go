package gate

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// Explicit request/response structs for account endpoints.

type gateAssetsRequest struct {
	Settle string `json:"settle"`
}

type gatePositionsRequest struct {
	Settle string `json:"settle"`
	Symbol string `json:"symbol,omitempty"`
}

// Private raw methods invoking HTTP requests.

func (c *Client) getRawAssets(ctx context.Context, req gateAssetsRequest) (*gateFuturesAccount, error) {
	var result gateFuturesAccount
	path := fmt.Sprintf("/futures/%s/accounts", req.Settle)
	err := c.sendRequest(ctx, "GET", path, nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) getRawPosition(ctx context.Context, req gatePositionsRequest) (*gatePosition, error) {
	var result gatePosition
	path := fmt.Sprintf("/futures/%s/positions/%s", req.Settle, req.Symbol)
	err := c.sendRequest(ctx, "GET", path, nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (c *Client) getRawPositions(ctx context.Context, req gatePositionsRequest) ([]gatePosition, error) {
	var result []gatePosition
	path := fmt.Sprintf("/futures/%s/positions", req.Settle)
	err := c.sendRequest(ctx, "GET", path, nil, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (c *Client) getRawPositionClose(ctx context.Context, settle, contract string) ([]gatePositionClose, error) {
	var result []gatePositionClose
	query := url.Values{}
	if contract != "" {
		query.Set("contract", contract)
	}
	query.Set("limit", "10")
	path := fmt.Sprintf("/futures/%s/position_close", settle)
	err := c.sendRequest(ctx, "GET", path, query, nil, &result)
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Public mapper methods implementing the exchange.AccountProvider & exchange.ClosedPnLProvider interfaces.

// GetAssets returns the account assets.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	resp, err := c.getRawAssets(ctx, gateAssetsRequest{Settle: gateSettleUsdt})
	if err != nil {
		return nil, fmt.Errorf("gate.io list assets: %w", err)
	}

	equity := decmath.ParseFloat(resp.Total)
	unrealized := decmath.ParseFloat(resp.UnrealisedPnl)
	asset := exchange.AssetInfo{
		Currency:         resp.Currency,
		PositionMargin:   decmath.ParseFloat(resp.PositionMargin),
		AvailableBalance: decmath.ParseFloat(resp.Available),
		Equity:           equity,
		Unrealized:       unrealized,
		CashBalance:      equity - unrealized,
	}
	return []exchange.AssetInfo{asset}, nil
}

// GetAssetByCurrency returns the account asset for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}

	for _, asset := range assets {
		if strings.EqualFold(asset.Currency, currency) {
			return &asset, nil
		}
	}

	return &exchange.AssetInfo{
		Currency: currency,
	}, nil
}

// GetOpenPositions returns all open positions, optionally filtered by symbol.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	if symbol != "" {
		pos, err := c.getRawPosition(ctx, gatePositionsRequest{
			Settle: gateSettleUsdt,
			Symbol: symbol,
		})
		if err != nil {
			if strings.Contains(err.Error(), "404") || strings.Contains(err.Error(), "not found") {
				return nil, nil
			}
			return nil, fmt.Errorf("gate.io get position for %s: %w", symbol, err)
		}

		if pos.Size == 0 {
			return nil, nil
		}
		return []exchange.Position{mapPosition(*pos)}, nil
	}

	rawPositions, err := c.getRawPositions(ctx, gatePositionsRequest{Settle: gateSettleUsdt})
	if err != nil {
		return nil, fmt.Errorf("gate.io list positions: %w", err)
	}

	positions := make([]exchange.Position, 0, len(rawPositions))
	for i := range rawPositions {
		pos := &rawPositions[i]
		if pos.Size != 0 {
			positions = append(positions, mapPosition(*pos))
		}
	}
	return positions, nil
}

// GetRecentClosedPnL queries position close history directly using a retry loop and maps the closed position metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	var closeHistory []gatePositionClose
	var matchedClose *gatePositionClose

	// Retry up to 5 times (with 1s delay) to allow the exchange to process the position closure.
	for attempt := 1; attempt <= 5; attempt++ {
		var err error
		closeHistory, err = c.getRawPositionClose(ctx, gateSettleUsdt, symbol)
		if err == nil {
			matchedClose = findMatchingCloseRecord(closeHistory, symbol, startTime)
		}

		if matchedClose != nil {
			break
		}

		// Sleep 1s before retrying, respecting context cancellation
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}

	if matchedClose == nil {
		return nil, fmt.Errorf("no matching position close record found in history for symbol %s (start time: %s)", symbol, startTime.Format(time.RFC3339))
	}

	pnlVal, _ := strconv.ParseFloat(matchedClose.Pnl, 64)
	pnlPnlVal, _ := strconv.ParseFloat(matchedClose.PnlPnl, 64)
	pnlFundVal, _ := strconv.ParseFloat(matchedClose.PnlFund, 64)
	pnlFeeVal, _ := strconv.ParseFloat(matchedClose.PnlFee, 64)
	longPriceVal, _ := strconv.ParseFloat(matchedClose.LongPrice, 64)
	shortPriceVal, _ := strconv.ParseFloat(matchedClose.ShortPrice, 64)
	closedSizeVal, _ := strconv.ParseFloat(matchedClose.AccumSize, 64)

	entryPrice := 0.0
	exitPrice := 0.0
	pnlRate := 0.0

	if matchedClose.Side == "long" {
		entryPrice = longPriceVal
		exitPrice = shortPriceVal
		if entryPrice > 0 {
			pnlRate = ((exitPrice - entryPrice) / entryPrice) * 100.0
		}
	} else {
		entryPrice = shortPriceVal
		exitPrice = longPriceVal
		if entryPrice > 0 {
			pnlRate = ((entryPrice - exitPrice) / entryPrice) * 100.0
		}
	}

	durationMs := int64(0)
	durationSec := int64(matchedClose.Time) - matchedClose.FirstOpenTime
	if durationSec > 0 {
		durationMs = durationSec * 1000
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     matchedClose.Contract,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: closedSizeVal,
		GrossPnL:   pnlPnlVal,
		Fee:        math.Abs(pnlFeeVal),
		FundingFee: pnlFundVal,
		DurationMs: durationMs,
		NetPnl:     pnlVal,
		PnLRate:    pnlRate,
	}, nil
}

// Helper mapping functions.

// mapPosition maps a gatePosition to exchange.Position.
func mapPosition(raw gatePosition) exchange.Position {
	pos := exchange.Position{
		Symbol:       raw.Contract,
		HoldVol:      float64(decmath.AbsInt64(raw.Size)),
		HoldAvgPrice: decmath.ParseFloat(raw.EntryPrice),
		OpenAvgPrice: decmath.ParseFloat(raw.EntryPrice),
	}

	if raw.Size > 0 {
		pos.PositionType = 1 // Long.
	} else if raw.Size < 0 {
		pos.PositionType = 2 // Short.
	}

	return pos
}

// findMatchingCloseRecord searches a slice of gatePositionClose for the newest matching close history item.
func findMatchingCloseRecord(closeHistory []gatePositionClose, symbol string, startTime time.Time) *gatePositionClose {
	for i := range closeHistory {
		item := &closeHistory[i]
		if item.Contract == symbol {
			if startTime.IsZero() || int64(item.Time) >= startTime.Unix() {
				return item
			}
		}
	}
	return nil
}
