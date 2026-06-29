package orangex

import (
	"context"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	params := map[string]any{
		"instrument_name": req.Symbol,
		"leverage":        req.Leverage,
	}
	resp, err := c.postRPC(ctx, "/private/adjust_perpetual_leverage", "/private/adjust_perpetual_leverage", params, true)
	if err != nil {
		return err
	}
	var envelope orangexRPCResponse[string]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	return nil
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	mType := "cross"
	if marginMode == "isolated" {
		mType = "isolate"
	}
	params := map[string]any{
		"instrument_name": symbol,
		"margin_type":     mType,
	}
	resp, err := c.postRPC(ctx, "/private/adjust_perpetual_margin_type", "/private/adjust_perpetual_margin_type", params, true)
	if err != nil {
		return err
	}
	var envelope orangexRPCResponse[string]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return err
	}
	if envelope.Error != nil {
		return envelope.Error
	}
	return nil
}
