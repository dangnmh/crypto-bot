package futures

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/exchange/mexc"
)

type mexcChangeLeverageRequest struct {
	Symbol       string `json:"symbol"`
	Leverage     int    `json:"leverage"`
	OpenType     int    `json:"openType"`     // 1: Isolated, 2: Cross
	PositionType int    `json:"positionType"` // 1: Long, 2: Short
}

func (c *Client) rawChangeLeverage(ctx context.Context, req mexcChangeLeverageRequest) error {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshal change leverage request: %w", err)
	}
	body, err := c.base.Request(ctx, http.MethodPost, "/api/v1/private/position/change_leverage", nil, bodyBytes, true)
	if err != nil {
		return err
	}
	_, err = mexc.ParseFuturesResponse[json.RawMessage](body)
	return err
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mexcReq := mexcChangeLeverageRequest{
		Symbol:       req.Symbol,
		Leverage:     req.Leverage,
		OpenType:     int(req.OpenType),
		PositionType: int(req.PositionType),
	}
	return c.rawChangeLeverage(ctx, mexcReq)
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for MEXC.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	openType := 1 // Isolated
	if marginMode == domain.MarginModeCross {
		openType = 2 // Cross
	}

	positionType := 1 // Long
	if side == domain.SideOpenShort || side == domain.SideCloseShort {
		positionType = 2 // Short
	}

	return c.rawChangeLeverage(ctx, mexcChangeLeverageRequest{
		Symbol:       symbol,
		Leverage:     leverage,
		OpenType:     openType,
		PositionType: positionType,
	})
}
