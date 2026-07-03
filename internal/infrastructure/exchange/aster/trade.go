package aster

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

func (c *Client) rawChangeLeverage(ctx context.Context, symbol string, leverage int) error {
	params := map[string]string{
		paramSymbol: symbol,
		"leverage":  strconv.Itoa(leverage),
	}
	_, err := c.request(ctx, http.MethodPost, "/fapi/v3/leverage", params, true)
	return err
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, symbol, marginType string) error {
	params := map[string]string{
		paramSymbol:  symbol,
		"marginType": marginType,
	}
	_, err := c.request(ctx, http.MethodPost, "/fapi/v3/marginType", params, true)
	return err
}

func (c *Client) rawSwitchPositionMode(ctx context.Context, dualSidePosition bool) error {
	params := map[string]string{
		"dualSidePosition": strconv.FormatBool(dualSidePosition),
	}
	_, err := c.request(ctx, http.MethodPost, "/fapi/v3/positionSide/dual", params, true)
	return err
}

func (c *Client) rawSwitchMultiAssetsMargin(ctx context.Context, multiAssetsMargin bool) error {
	params := map[string]string{
		"multiAssetsMargin": strconv.FormatBool(multiAssetsMargin),
	}
	_, err := c.request(ctx, http.MethodPost, "/fapi/v3/multiAssetsMargin", params, true)
	return err
}

func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	err := c.rawChangeLeverage(ctx, req.Symbol, req.Leverage)
	if err != nil {
		var apiErr *exchange.APIError
		if errors.As(err, &apiErr) && apiErr.Code == -5018 {
			c.logger.WarnContext(ctx, "Ignore leverage adjustment limit warning", "symbol", req.Symbol, "leverage", req.Leverage, "error", err)
			return nil
		}
		return err
	}
	return nil
}

func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	mType := marginModeIsolated
	if marginMode == domain.MarginModeCross {
		mType = marginModeCross
	}
	err := c.rawSwitchMarginMode(ctx, symbol, mType)
	if err != nil {
		if apiErr, ok := errors.AsType[*exchange.APIError](err); ok {
			if apiErr.Code == -4046 {
				c.logger.WarnContext(ctx, "Ignore margin mode adjustment: No need to change margin type", "symbol", symbol, "marginMode", marginMode)
				return nil
			}
			if apiErr.Code == -4168 {
				if switchErr := c.rawSwitchMultiAssetsMargin(ctx, false); switchErr != nil {
					return fmt.Errorf("failed to disable multi-assets mode: %w (original error: %s)", switchErr, err.Error())
				}
				err2 := c.rawSwitchMarginMode(ctx, symbol, mType)
				if err2 != nil {
					var apiErr2 *exchange.APIError
					if errors.As(err2, &apiErr2) && apiErr2.Code == -4046 {
						c.logger.WarnContext(ctx, "Ignore margin mode adjustment: No need to change margin type", "symbol", symbol, "marginMode", marginMode)
						return nil
					}
					return err2
				}
				return nil
			}
		}
		return err
	}
	return nil
}

func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	dual := positionMode == domain.PositionModeHedge
	err := c.rawSwitchPositionMode(ctx, dual)
	if err != nil {
		var apiErr *exchange.APIError
		if errors.As(err, &apiErr) && apiErr.Code == -4059 {
			c.logger.WarnContext(ctx, "Ignore position mode adjustment: No need to change position side", "positionMode", positionMode)
			return nil
		}
		return err
	}
	return nil
}
