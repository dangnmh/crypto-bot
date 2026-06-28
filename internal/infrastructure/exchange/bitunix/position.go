package bitunix

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/xjson"
)

type bitunixRawPosition struct {
	PositionID       string `json:"positionId"`
	Symbol           string `json:"symbol"`
	Side             string `json:"side"` // "LONG", "SHORT", "BUY", "SELL"
	OpenPrice        string `json:"openPrice"`
	AvgOpenPrice     string `json:"avgOpenPrice"`
	Size             string `json:"size"`
	Qty              string `json:"qty"`
	Leverage         int    `json:"leverage"`
	UnrealizedProfit string `json:"unrealizedProfit"`
	UnrealizedPnL    string `json:"unrealizedPnL"`
	UnrealizedPNL    string `json:"unrealizedPNL"`
}

func (p *bitunixRawPosition) isMatch(closeSide domain.Side) bool {
	isLong := strings.EqualFold(p.Side, "LONG") || strings.EqualFold(p.Side, "BUY")
	isShort := strings.EqualFold(p.Side, "SHORT") || strings.EqualFold(p.Side, "SELL")

	if closeSide == domain.SideCloseLong && isLong {
		return true
	}
	if closeSide == domain.SideCloseShort && isShort {
		return true
	}
	return false
}

func (c *Client) rawGetPendingPositions(ctx context.Context, symbol string) ([]bitunixRawPosition, error) {
	query := make(map[string]string)
	if symbol != "" {
		query[paramSymbol] = symbol
	}
	body, err := c.request(ctx, http.MethodGet, "/api/v1/futures/position/get_pending_positions", query, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int                  `json:"code"`
		Data []bitunixRawPosition `json:"data"`
		Msg  string               `json:"msg"`
	}
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal pending positions: %w", err)
	}

	return resp.Data, nil
}

// GetOpenPositions returns all open futures positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	raw, err := c.rawGetPendingPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(raw))
	for i := range raw {
		p := &raw[i]

		size := decmath.ParseFloat(p.Qty)
		if size == 0.0 {
			size = decmath.ParseFloat(p.Size)
		}
		if size == 0.0 {
			continue
		}

		openPrice := decmath.ParseFloat(p.AvgOpenPrice)
		if openPrice == 0.0 {
			openPrice = decmath.ParseFloat(p.OpenPrice)
		}

		pnl := decmath.ParseFloat(p.UnrealizedPNL)
		if pnl == 0.0 {
			pnl = decmath.ParseFloat(p.UnrealizedPnL)
		}
		if pnl == 0.0 {
			pnl = decmath.ParseFloat(p.UnrealizedProfit)
		}

		posType := exchange.PositionTypeLong
		if strings.EqualFold(p.Side, "SHORT") || strings.EqualFold(p.Side, "SELL") {
			posType = exchange.PositionTypeShort
		}

		positions = append(positions, exchange.Position{
			Symbol:          p.Symbol,
			HoldVol:         size,
			PositionType:    posType,
			OpenAvgPrice:    openPrice,
			HoldAvgPrice:    openPrice,
			CloseProfitLoss: pnl,
			Leverage:        p.Leverage,
		})
	}

	return positions, nil
}

// ClosePosition closes a position with market order.
func (c *Client) ClosePosition(
	ctx context.Context,
	symbol string,
	closeSide domain.Side,
	volume float64,
	positionMode domain.PositionMode,
	leverage int,
) error {
	rawPositions, err := c.rawGetPendingPositions(ctx, symbol)
	if err != nil {
		return err
	}

	var targetPos *bitunixRawPosition
	for i := range rawPositions {
		p := &rawPositions[i]
		if strings.EqualFold(p.Symbol, symbol) && p.isMatch(closeSide) {
			targetPos = p
			break
		}
	}

	if targetPos == nil {
		// Position is already closed
		return nil
	}

	qtyToClose := targetPos.Qty
	if qtyToClose == "" {
		qtyToClose = targetPos.Size
	}
	if volume > 0 {
		qtyToClose = strconv.FormatFloat(volume, 'f', -1, 64)
	}

	sideStr := sideBuy
	if closeSide == domain.SideCloseLong {
		sideStr = sideSell
	}

	body := map[string]any{
		paramSymbol:    symbol,
		paramQty:       qtyToClose,
		paramSide:      sideStr,
		paramTradeSide: tradeSideClose,
		paramOrderType: orderTypeMarket,
		"positionId":   targetPos.PositionID,
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/trade/place_order", nil, body)
	if err != nil {
		return err
	}

	var resp bitunixPlaceOrderResp
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal close position place order response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("close position failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}

// CloseAllPositions closes all open positions for a symbol natively.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	return c.rawCloseAllPositions(ctx, symbol)
}

func (c *Client) rawCloseAllPositions(ctx context.Context, symbol string) error {
	body := make(map[string]any)
	if symbol != "" {
		body[paramSymbol] = symbol
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/trade/close_all_position", nil, body)
	if err != nil {
		return err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal close all position response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("close all position failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}

type bitunixChangePositionModeRequest struct {
	PositionMode string `json:"positionMode"` // "HEDGE" or "ONE_WAY"
}

// SwitchPositionMode switches hold mode between hedge and one-way.
func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	modeStr := "HEDGE"
	if positionMode == domain.PositionModeOneWay {
		modeStr = "ONE_WAY"
	}
	return c.rawSwitchPositionMode(ctx, modeStr)
}

func (c *Client) rawSwitchPositionMode(ctx context.Context, positionMode string) error {
	body := bitunixChangePositionModeRequest{
		PositionMode: positionMode,
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/account/change_position_mode", nil, body)
	if err != nil {
		return err
	}

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal change position mode response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("change position mode failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}
