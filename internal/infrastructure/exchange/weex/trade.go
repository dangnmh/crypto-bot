package weex

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

// ChangeLeverage changes leverage for a symbol on WEEX.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	mgnType := marginTypeCross
	if req.OpenType == domain.OpenTypeIsolated {
		mgnType = marginTypeIsolated
	}
	levStr := strconv.Itoa(req.Leverage)
	body := map[string]any{
		keySymbol:               req.Symbol,
		keyMarginType:           mgnType,
		"crossLeverage":         levStr,
		"isolatedLongLeverage":  levStr,
		"isolatedShortLeverage": levStr,
	}
	resBytes, err := c.request(ctx, http.MethodPost, "/capi/v3/account/leverage", nil, body, true)
	if err != nil {
		return err
	}
	_, err = parseResponse[any](resBytes)
	return err
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED).
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	mgnType := marginTypeCross
	if marginMode == marginTypeIsolated {
		mgnType = marginTypeIsolated
	}
	body := map[string]any{
		keySymbol:       symbol,
		keyMarginType:   mgnType,
		"separatedType": separatedTypeCb,
	}
	resBytes, err := c.request(ctx, http.MethodPost, "/capi/v3/account/marginType", nil, body, true)
	if err != nil {
		var apiErr *exchange.APIError
		if errors.As(err, &apiErr) && apiErr.Code == -1054 {
			// Fallback to SEPARATED if COMBINED is not supported by the contract
			body["separatedType"] = separatedTypeSp
			resBytes, err = c.request(ctx, http.MethodPost, "/capi/v3/account/marginType", nil, body, true)
			if err != nil {
				return err
			}
		} else {
			return err
		}
	}
	_, err = parseResponse[any](resBytes)
	return err
}
