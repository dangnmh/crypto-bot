package orangex

import (
	"context"
	"log/slog"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type adjustLeverageParams struct {
	InstrumentName string `json:"instrument_name"`
	Leverage       int    `json:"leverage"`
}

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	params := adjustLeverageParams{
		InstrumentName: req.Symbol,
		Leverage:       req.Leverage,
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
		if envelope.Error.Code == 5147 || strings.Contains(envelope.Error.Message, "position mode") {
			c.logger.WarnContext(ctx, "OrangeX adjust leverage not supported on current position mode (ignored)", slog.String("symbol", req.Symbol))
			return nil
		}
		return envelope.Error
	}
	return nil
}

type MarginType string

const (
	MarginTypeCross    MarginType = "cross"
	MarginTypeIsolated MarginType = "isolate"
)

type adjustMarginTypeParams struct {
	InstrumentName string     `json:"instrument_name"`
	MarginType     MarginType `json:"margin_type"`
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	mType := MarginTypeCross
	if strings.EqualFold(string(marginMode), string(domain.MarginModeIsolated)) {
		mType = MarginTypeIsolated
	}
	params := adjustMarginTypeParams{
		InstrumentName: symbol,
		MarginType:     mType,
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
