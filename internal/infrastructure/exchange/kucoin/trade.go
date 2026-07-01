package kucoin

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type kucoinSwitchMarginModeRequest struct {
	Symbol     string `json:"symbol"`
	MarginMode string `json:"marginMode"`
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, req kucoinSwitchMarginModeRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("kucoin marshal switch margin mode request: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/api/v2/position/changeMarginMode", nil, bodyBytes)
	if err != nil {
		return err
	}
	return ParseResponseIgnoreData(body, "changeMarginMode")
}

// ChangeLeverage changes the leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	return errors.New("not implemented")
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for KuCoin.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	return c.rawSwitchMarginMode(ctx, kucoinSwitchMarginModeRequest{
		Symbol:     symbol,
		MarginMode: string(marginMode),
	})
}
