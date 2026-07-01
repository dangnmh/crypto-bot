package toobit

import (
	"context"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
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
