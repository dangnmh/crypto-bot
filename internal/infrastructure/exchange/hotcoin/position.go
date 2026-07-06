package hotcoin

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

// GetOpenPositions returns the user's active open positions.
// Since Hotcoin REST API does not have an endpoint to list positions, we return an empty list gracefully
// and rely on WebSocket pushes (which update the in-memory store in real-time).
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	c.logger.DebugContext(ctx, "GetOpenPositions called, returning empty list (relying on WebSocket updates)", "symbol", symbol)
	return []exchange.Position{}, nil
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
