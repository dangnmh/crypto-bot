package okx

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type okxSetLeverageRequest struct {
	InstID  string `json:"instId"`
	Lever   string `json:"lever"`
	MgnMode string `json:"mgnMode"`
	PosSide string `json:"posSide,omitempty"`
}

func (c *Client) rawSetLeverage(ctx context.Context, req okxSetLeverageRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("okx marshal set leverage request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/api/v5/account/set-leverage", nil, bodyBytes)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "set_leverage")
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mgnMode := modeIsolated
	if req.OpenType == exchange.OpenTypeCross {
		mgnMode = modeCross
	}

	posSide := ""
	if mgnMode == modeIsolated {
		switch req.PositionType {
		case exchange.PositionTypeLong:
			posSide = posSideLong
		case exchange.PositionTypeShort:
			posSide = posSideShort
		case exchange.PositionTypeUnknown:
			// Default to empty posSide.
		}
	}

	err := c.rawSetLeverage(ctx, okxSetLeverageRequest{
		InstID:  req.Symbol,
		Lever:   fmt.Sprintf("%d", req.Leverage),
		MgnMode: mgnMode,
		PosSide: posSide,
	})
	if err != nil {
		var apiErr *exchange.APIError
		if errors.As(err, &apiErr) && apiErr.Code == 51000 {
			return c.rawSetLeverage(ctx, okxSetLeverageRequest{
				InstID:  req.Symbol,
				Lever:   fmt.Sprintf("%d", req.Leverage),
				MgnMode: mgnMode,
				PosSide: "",
			})
		}
		return err
	}
	return nil
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for OKX.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}
