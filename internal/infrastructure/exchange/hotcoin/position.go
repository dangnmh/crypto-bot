package hotcoin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type hotcoinPosition struct {
	Amount            xjson.Number `json:"amount"`
	ContractCode      string       `json:"contractCode"`
	Side              string       `json:"side"`
	Price             xjson.Number `json:"price"`
	Fee               xjson.Number `json:"fee"`
	Lever             xjson.Number `json:"lever"`
	RealizedSurplus   xjson.Number `json:"realizedSurplus"`
	UnRealizedSurplus xjson.Number `json:"unRealizedSurplus"`
}

func (p *hotcoinPosition) toPosition() exchange.Position {
	pType := exchange.PositionTypeLong
	if strings.EqualFold(p.Side, "short") {
		pType = exchange.PositionTypeShort
	}

	// Normalize symbol from contractCode (e.g. btcusdt -> BTC_USDT)
	symbol := strings.ToUpper(p.ContractCode)
	if !strings.Contains(symbol, "_") {
		if before, ok := strings.CutSuffix(symbol, "USDT"); ok {
			symbol = before + "_USDT"
		} else if before, ok := strings.CutSuffix(symbol, "USDC"); ok {
			symbol = before + "_USDC"
		}
	}

	vol := xjson.ToFloat64(p.Amount)
	priceVal := xjson.ToFloat64(p.Price)
	feeVal := xjson.ToFloat64(p.Fee)
	leverVal := int(xjson.ToInt64(p.Lever))

	return exchange.Position{
		Symbol:       symbol,
		HoldVol:      vol,
		PositionType: pType,
		OpenAvgPrice: priceVal,
		HoldAvgPrice: priceVal,
		Leverage:     leverVal,
		Fee:          feeVal,
	}
}

// GetOpenPositions returns the user's active open positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	if symbol == "" {
		return nil, fmt.Errorf("symbol is required for Hotcoin GetOpenPositions")
	}

	body, err := c.GetOpenPositionsRaw(ctx, map[string]string{"symbol": symbol})
	if err != nil {
		return nil, err
	}

	var rawPositions []hotcoinPosition
	trimmed := strings.TrimSpace(string(body))

	if strings.HasPrefix(trimmed, "[") {
		if err := json.Unmarshal(body, &rawPositions); err != nil {
			return nil, fmt.Errorf("unmarshal positions list array: %w", err)
		}
	} else {
		var wrapped struct {
			Code int               `json:"code"`
			Msg  string            `json:"msg"`
			Data []hotcoinPosition `json:"data"`
		}
		if err := json.Unmarshal(body, &wrapped); err != nil {
			return nil, fmt.Errorf("unmarshal positions list wrapped: %w", err)
		}
		if wrapped.Code != 200 && wrapped.Msg != "success" && wrapped.Msg != "" {
			return nil, fmt.Errorf("API error: code=%d msg=%s", wrapped.Code, wrapped.Msg)
		}
		rawPositions = wrapped.Data
	}

	var positions []exchange.Position
	for i := range rawPositions {
		pos := rawPositions[i].toPosition()
		if pos.HoldVol == 0 {
			continue
		}
		positions = append(positions, pos)
	}

	return positions, nil
}

// ClosePosition closes a long or short position at the market price using Hotcoin's native closePosition endpoint.
func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	var side string
	switch closeSide {
	case domain.SideCloseLong:
		side = sideLong
	case domain.SideCloseShort:
		side = sideShort
	default:
		return fmt.Errorf("unsupported close side: %v", closeSide)
	}

	path := fmt.Sprintf("/api/v1/perpetual/products/%s/%s/closePosition", contractCode, side)
	_, err := c.request(ctx, http.MethodPost, path, nil, nil, true)
	return err
}

// CloseAllPositions closes all positions (both long and short) for a symbol using Hotcoin's native closePosition endpoint.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))

	// Close Long positions
	pathLong := fmt.Sprintf("/api/v1/perpetual/products/%s/long/closePosition", contractCode)
	if _, err := c.request(ctx, http.MethodPost, pathLong, nil, nil, true); err != nil {
		c.logger.ErrorContext(ctx, "failed to close all long positions", "symbol", symbol, "error", err)
	}

	// Close Short positions
	pathShort := fmt.Sprintf("/api/v1/perpetual/products/%s/short/closePosition", contractCode)
	if _, err := c.request(ctx, http.MethodPost, pathShort, nil, nil, true); err != nil {
		c.logger.ErrorContext(ctx, "failed to close all short positions", "symbol", symbol, "error", err)
	}

	return nil
}
