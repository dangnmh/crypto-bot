package toobit

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

// Private raw methods.

func (c *Client) rawChangeLeverage(ctx context.Context, symbol string, leverage int) ([]byte, error) {
	params := map[string]string{
		symbolKey:  symbol,
		"leverage": strconv.Itoa(leverage),
	}
	body, err := c.request(ctx, http.MethodPost, "/api/v2/futures/leverage", params, true)
	if err != nil {
		return nil, err
	}
	return body, nil
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, symbol, marginMode string) ([]byte, error) {
	params := map[string]string{
		symbolKey:    symbol,
		"marginType": marginMode,
	}
	body, err := c.request(ctx, http.MethodPost, "/api/v1/futures/marginType", params, true)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// Public mapper methods.

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	body, err := c.rawChangeLeverage(ctx, req.Symbol, req.Leverage)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED).
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	mgnType := "CROSS"
	if marginMode == domain.MarginModeIsolated {
		mgnType = marginIsolated
	}
	body, err := c.rawSwitchMarginMode(ctx, symbol, mgnType)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](body)
	return err
}

type toobitRiskLimitConfig struct {
	Level          int          `json:"level"`
	Quantity       string       `json:"quantity"`
	MaintainMargin string       `json:"maintainMargin"`
	InitialMargin  string       `json:"initialMargin"`
	MaxLeverage    xjson.Number `json:"maxLeverage"`
}

func (c *Client) rawGetRiskLimits(ctx context.Context, symbol string) ([]byte, error) {
	params := map[string]string{
		symbolKey: symbol,
	}
	return c.request(ctx, http.MethodGet, "/api/v1/futures/riskLimits", params, false)
}

// GetMaxLeverage queries risk limits for the specified symbol and returns the maximum leverage allowed.
func (c *Client) GetMaxLeverage(ctx context.Context, symbol string) (int, error) {
	body, err := c.rawGetRiskLimits(ctx, symbol)
	if err != nil {
		return 0, err
	}
	limits, err := parseResponse[[]toobitRiskLimitConfig](body)
	if err != nil {
		return 0, err
	}
	maxLev := 0
	for _, rl := range limits {
		val, err := rl.MaxLeverage.Float64()
		if err == nil && int(val) > maxLev {
			maxLev = int(val)
		}
	}
	if maxLev == 0 {
		return 0, fmt.Errorf("no valid risk limits found for symbol %s", symbol)
	}
	return maxLev, nil
}
