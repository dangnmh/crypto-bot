package hotcoin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

// ChangeLeverage modifies leverage for a specific symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	var openTypeVal int
	if req.OpenType == domain.OpenTypeIsolated {
		openTypeVal = 1
	} else {
		openTypeVal = 0
	}

	var sideStr string
	if req.PositionType == exchange.PositionTypeShort {
		sideStr = sideShort
	} else {
		sideStr = sideLong
	}

	return c.setLever(ctx, req.Symbol, openTypeVal, req.Leverage, sideStr)
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED).
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	// leverage on ChangeLeverage
	return nil
}

func (c *Client) setLever(ctx context.Context, symbol string, openTypeVal, leverage int, sideStr string) error {
	contractCode := strings.ToLower(strings.ReplaceAll(symbol, "_", ""))
	path := fmt.Sprintf("/api/v1/perpetual/position/%s/lever", contractCode)

	body := map[string]any{
		"type":  openTypeVal, // Cross: 0, Isolated: 1
		"lever": leverage,    // 1~125
		"side":  sideStr,     // "long" or "short"
	}

	_, err := c.request(ctx, http.MethodPost, path, nil, body, true)
	if err != nil {
		return fmt.Errorf("set leverage for %s: %w", symbol, err)
	}
	return nil
}
