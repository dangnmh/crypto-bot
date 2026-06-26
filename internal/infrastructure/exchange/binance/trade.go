package binance

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type binanceChangeLeverageRequest struct {
	Symbol   string
	Leverage int64
}

type binanceSwitchMarginModeRequest struct {
	Symbol     string
	MarginType string
}

func (c *Client) rawChangeLeverage(ctx context.Context, req binanceChangeLeverageRequest) (*changeLeverageResponse, error) {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["leverage"] = req.Leverage

	var resp changeLeverageResponse
	err := c.request(ctx, http.MethodPost, "/fapi/v1/leverage", params, true, &resp)
	if err != nil {
		return nil, fmt.Errorf("binance change leverage: %w", err)
	}
	return &resp, nil
}

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	_, err := c.rawChangeLeverage(ctx, binanceChangeLeverageRequest{
		Symbol:   req.Symbol,
		Leverage: int64(req.Leverage),
	})
	return err
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, req binanceSwitchMarginModeRequest) error {
	params := make(map[string]any)
	params["symbol"] = req.Symbol
	params["marginType"] = req.MarginType

	err := c.request(ctx, http.MethodPost, "/fapi/v1/marginType", params, true, nil)
	if err != nil {
		if apiErr, ok := exchange.IsAPIError(err); ok && (apiErr.Code == -4046 || strings.Contains(strings.ToLower(apiErr.Message), "no need to change")) {
			return nil
		}
		return err
	}
	return nil
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Binance.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	var mode = "ISOLATED"
	if marginMode == "CROSS" {
		mode = "CROSSED"
	}

	return c.rawSwitchMarginMode(ctx, binanceSwitchMarginModeRequest{
		Symbol:     symbol,
		MarginType: mode,
	})
}
