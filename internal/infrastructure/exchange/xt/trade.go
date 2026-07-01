package xt

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type xtTradeResponse struct {
	ReturnCode int64  `json:"returnCode"`
	MsgInfo    string `json:"msgInfo"`
}

// ChangeLeverage satisfies the Client interface.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	sym := strings.ToLower(req.Symbol)
	if !strings.Contains(sym, "_") {
		if before, ok := strings.CutSuffix(sym, "usdt"); ok {
			sym = before + "_usdt"
		} else if before, ok := strings.CutSuffix(sym, "usdc"); ok {
			sym = before + "_usdc"
		}
	}

	// Change leverage for both LONG and SHORT position sides
	sides := []string{sideLong, sideShort}
	for _, ps := range sides {
		params := map[string]string{
			paramSymbol:       sym,
			paramPositionSide: ps,
			"leverage":        strconv.Itoa(req.Leverage),
		}

		respBytes, err := c.request(ctx, "POST", "/future/user/v1/position/adjust-leverage", params, nil, true)
		if err != nil {
			return fmt.Errorf("adjust leverage request error for %s: %w", ps, err)
		}

		var resp xtTradeResponse
		if err := xjson.Unmarshal(respBytes, &resp); err != nil {
			return fmt.Errorf("unmarshal adjust leverage response for %s: %w", ps, err)
		}

		// If one side succeeds or fails, log/handle it (leverage adjustment might fail if there's no open position,
		// or if the value is already identical. We shouldn't fail the whole execution if it's already set).
		if resp.ReturnCode != 0 && !strings.Contains(resp.MsgInfo, "identical") {
			c.logger.Warn("Failed to adjust leverage for position side", "side", ps, "code", resp.ReturnCode, "msg", resp.MsgInfo)
		}
	}

	return nil
}

// SwitchMarginMode satisfies the Client interface.
func (c *Client) SwitchMarginMode(
	ctx context.Context,
	symbol string,
	marginMode domain.MarginMode,
	leverage int,
	side domain.Side,
) error {
	sym := strings.ToLower(symbol)
	if !strings.Contains(sym, "_") {
		if before, ok := strings.CutSuffix(sym, "usdt"); ok {
			sym = before + "_usdt"
		} else if before, ok := strings.CutSuffix(sym, "usdc"); ok {
			sym = before + "_usdc"
		}
	}

	// Map marginMode to CROSSED / ISOLATED
	var pt string
	if marginMode == domain.MarginModeIsolated {
		pt = modeIsolated
	} else {
		pt = modeCrossed
	}

	// Map side to LONG / SHORT
	var ps string
	if side == domain.SideOpenLong || side == domain.SideCloseLong {
		ps = sideLong
	} else {
		ps = sideShort
	}

	params := map[string]string{
		paramSymbol:       sym,
		paramPositionSide: ps,
		paramPositionType: pt,
	}

	respBytes, err := c.request(ctx, "POST", "/future/user/v1/position/change-type", params, nil, true)
	if err != nil {
		return err
	}

	var resp xtTradeResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal change margin type response: %w", err)
	}

	// We ignore "identical" errors if the mode is already correct
	if resp.ReturnCode != 0 && !strings.Contains(resp.MsgInfo, "identical") && !strings.Contains(resp.MsgInfo, "no change") {
		return fmt.Errorf("change margin type API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return nil
}
