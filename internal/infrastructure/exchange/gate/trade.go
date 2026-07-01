package gate

import (
	"context"
	"fmt"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type gateChangeLeverageRequest struct {
	Symbol             string `json:"symbol"`
	Leverage           string `json:"leverage"`
	CrossLeverageLimit string `json:"cross_leverage_limit,omitempty"`
}

type gatePositionCrossModeRequest struct {
	Mode     string `json:"mode"`
	Contract string `json:"contract"`
}

// Private raw methods.

func (c *Client) rawChangeLeverage(ctx context.Context, settle string, req gateChangeLeverageRequest) error {
	params := map[string]string{
		"leverage": req.Leverage,
	}
	if req.CrossLeverageLimit != "" {
		params["cross_leverage_limit"] = req.CrossLeverageLimit
	}

	path := fmt.Sprintf("/futures/%s/positions/%s/leverage", settle, req.Symbol)
	_, err := c.RawRequest(ctx, "POST", path, params, nil)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/%s/leverage", settle, req.Symbol)
		_, errDual := c.RawRequest(ctx, "POST", dualPath, params, nil)
		if errDual != nil {
			return fmt.Errorf("gate.io update leverage error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, settle string, req gatePositionCrossModeRequest) error {
	bodyBytes, err := xjson.Marshal(&req)
	if err != nil {
		return err
	}

	path := fmt.Sprintf("/futures/%s/positions/cross_mode", settle)
	_, err = c.RawRequest(ctx, "POST", path, nil, bodyBytes)
	if err != nil {
		dualPath := fmt.Sprintf("/futures/%s/dual_comp/positions/cross_mode", settle)
		_, errDual := c.RawRequest(ctx, "POST", dualPath, nil, bodyBytes)
		if errDual != nil {
			return fmt.Errorf("gate.io update margin mode error (standard: %s, dual: %w)", err.Error(), errDual)
		}
	}
	return nil
}

// Public mapper methods.

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	var leverageStr string
	var crossLimitStr string

	if req.OpenType == exchange.OpenTypeCross {
		leverageStr = "0"
		crossLimitStr = fmt.Sprintf("%d", req.Leverage)
	} else {
		leverageStr = fmt.Sprintf("%d", req.Leverage)
		crossLimitStr = ""
	}

	return c.rawChangeLeverage(ctx, gateSettleUsdt, gateChangeLeverageRequest{
		Symbol:             req.Symbol,
		Leverage:           leverageStr,
		CrossLeverageLimit: crossLimitStr,
	})
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Gate.io.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	settle := gateSettleUsdt
	modeStr := gateMarginModeIsolated
	if marginMode == domain.MarginModeCross {
		modeStr = gateMarginModeCross
	}

	return c.rawSwitchMarginMode(ctx, settle, gatePositionCrossModeRequest{
		Contract: symbol,
		Mode:     modeStr,
	})
}
