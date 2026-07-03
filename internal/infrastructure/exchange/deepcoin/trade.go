package deepcoin

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

// Private raw methods.

func (c *Client) rawSetLeverage(ctx context.Context, bodyMap map[string]any) (*deepcoinOrderResultData, error) {
	body, err := c.PostCtx(ctx, "/deepcoin/account/set-leverage", bodyMap)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinOrderResultData](body, "set_leverage")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public TradeProvider methods.

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mgnType := mgnModeCross
	if req.OpenType == domain.OpenTypeIsolated {
		mgnType = mgnModeIsolated
	}
	bodyMap := map[string]any{
		paramInstId:      req.Symbol,
		paramLever:       strconv.Itoa(req.Leverage),
		paramMgnMode:     mgnType,
		paramMrgPosition: mrgPositionMerge,
	}

	res, err := c.rawSetLeverage(ctx, bodyMap)
	if err != nil {
		return err
	}
	if res.SCode != "0" {
		return fmt.Errorf("set leverage failed: %s", res.SMsg)
	}
	return nil
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	return nil
}
