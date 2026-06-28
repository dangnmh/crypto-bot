package bitunix

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bitunixChangeLeverageRequest struct {
	Symbol     string `json:"symbol"`
	Leverage   int    `json:"leverage"`
	MarginCoin string `json:"marginCoin"`
}

type bitunixChangeLeverageData struct {
	Symbol     string `json:"symbol"`
	Leverage   int    `json:"leverage"`
	MarginCoin string `json:"marginCoin"`
}

type bitunixChangeLeverageResp struct {
	Code int                       `json:"code"`
	Msg  string                    `json:"msg"`
	Data bitunixChangeLeverageData `json:"data"`
}

type bitunixChangeMarginModeData struct {
	MarginMode string `json:"marginMode"`
}

type bitunixChangeMarginModeResp struct {
	Code int                         `json:"code"`
	Msg  string                      `json:"msg"`
	Data bitunixChangeMarginModeData `json:"data"`
}

// ChangeLeverage sets the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	marginCoin := coinUSDT
	if strings.HasSuffix(strings.ToUpper(req.Symbol), coinUSDC) {
		marginCoin = coinUSDC
	}
	return c.rawChangeLeverage(ctx, req.Symbol, req.Leverage, marginCoin)
}

func (c *Client) rawChangeLeverage(ctx context.Context, symbol string, leverage int, marginCoin string) error {
	body := bitunixChangeLeverageRequest{
		Symbol:     symbol,
		Leverage:   leverage,
		MarginCoin: marginCoin,
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/account/change_leverage", nil, body)
	if err != nil {
		return err
	}

	var resp bitunixChangeLeverageResp
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal change leverage response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("change leverage failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}

// SwitchMarginMode changes the margin mode for a symbol (Isolated vs Cross).
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	modeStr := "ISOLATION"
	if strings.EqualFold(marginMode, "CROSS") {
		modeStr = "CROSS"
	}

	marginCoin := coinUSDT
	if strings.HasSuffix(strings.ToUpper(symbol), coinUSDC) {
		marginCoin = coinUSDC
	}
	return c.rawSwitchMarginMode(ctx, symbol, modeStr, marginCoin)
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, symbol, marginMode, marginCoin string) error {
	body := map[string]any{
		paramSymbol:  symbol,
		"marginCoin": marginCoin,
		"marginMode": marginMode,
	}

	bodyBytes, err := c.request(ctx, http.MethodPost, "/api/v1/futures/account/change_margin_mode", nil, body)
	if err != nil {
		return err
	}

	var resp bitunixChangeMarginModeResp
	if err := xjson.Unmarshal(bodyBytes, &resp); err != nil {
		return fmt.Errorf("unmarshal change margin mode response: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("change margin mode failed code %d: %s", resp.Code, resp.Msg)
	}

	return nil
}
