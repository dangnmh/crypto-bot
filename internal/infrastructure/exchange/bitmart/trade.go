package bitmart

import (
	"context"
	"net/http"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

// Private raw methods.

func (c *Client) rawSubmitLeverage(ctx context.Context, body []byte) ([]byte, error) {
	return c.requestFull(ctx, http.MethodPost, "/contract/private/submit-leverage", nil, body, true)
}

// Public mapper methods.

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	openTypeStr := openTypeIsolated
	if req.OpenType == domain.OpenTypeCross {
		openTypeStr = openTypeCross
	}
	bodyMap := map[string]any{
		paramSymbol: req.Symbol,
		"leverage":  strconv.Itoa(req.Leverage),
		"open_type": openTypeStr,
	}
	bodyBytes, err := xjson.Marshal(bodyMap)
	if err != nil {
		return err
	}
	_, err = c.rawSubmitLeverage(ctx, bodyBytes)
	return err
}

// SwitchMarginMode sets margin mode (CROSS or ISOLATED). Bitmart does it per order so this is a no-op.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	return nil
}
