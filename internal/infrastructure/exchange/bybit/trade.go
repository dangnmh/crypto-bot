package bybit

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type bybitChangeLeverageRequest struct {
	Category     string `json:"category"`
	Symbol       string `json:"symbol"`
	BuyLeverage  string `json:"buyLeverage"`
	SellLeverage string `json:"sellLeverage"`
}

type bybitSwitchIsolatedRequest struct {
	Category     string `json:"category"`
	Symbol       string `json:"symbol"`
	TradeMode    int    `json:"tradeMode"` // 0: cross, 1: isolated
	BuyLeverage  string `json:"buyLeverage"`
	SellLeverage string `json:"sellLeverage"`
}

type bybitSetMarginModeRequest struct {
	SetMarginMode string `json:"setMarginMode"` // REGULAR_MARGIN, ISOLATED_MARGIN
}

func (c *Client) rawChangeLeverage(ctx context.Context, req bybitChangeLeverageRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("bybit change leverage marshal: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/v5/position/set-leverage", nil, bodyBytes)
	if err != nil {
		return fmt.Errorf("bybit change leverage: %w", err)
	}
	var resp bybitResponse[any]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit change leverage json unmarshal: %w", err)
	}
	if resp.RetCode != 0 && resp.RetCode != 110043 {
		return fmt.Errorf("bybit change leverage error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

func (c *Client) rawSwitchIsolatedMode(ctx context.Context, req bybitSwitchIsolatedRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("bybit switch isolated mode marshal: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/v5/position/switch-isolated", nil, bodyBytes)
	if err != nil {
		return fmt.Errorf("bybit switch isolated mode: %w", err)
	}
	var resp bybitResponse[any]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit switch isolated mode json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110026 || strings.Contains(strings.ToLower(resp.RetMsg), "already") {
			return nil
		}
		// Fallback for unified account
		if resp.RetCode == 100028 || strings.Contains(strings.ToLower(resp.RetMsg), "unified account is forbidden") {
			c.logger.InfoContext(ctx, "Bybit SwitchPositionMargin returned unified account restriction, falling back to SetMarginMode", slog.String("symbol", req.Symbol))
			marginMode := constantCross
			if req.TradeMode == 1 {
				marginMode = "ISOLATED"
			}
			return c.switchUnifiedMarginMode(ctx, marginMode)
		}
		return fmt.Errorf("bybit switch isolated mode error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

//nolint:dupl // Structurally similar to rawSwitchPositionMode and rawSetMarginMode.
func (c *Client) rawSetMarginMode(ctx context.Context, req bybitSetMarginModeRequest) error {
	bodyBytes, err := xjson.Marshal(req)
	if err != nil {
		return fmt.Errorf("bybit set account margin mode marshal: %w", err)
	}
	body, err := c.RawRequest(ctx, http.MethodPost, "/v5/account/set-margin-mode", nil, bodyBytes)
	if err != nil {
		return fmt.Errorf("bybit set account margin mode: %w", err)
	}
	var resp bybitResponse[any]
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("bybit set account margin mode json unmarshal: %w", err)
	}
	if resp.RetCode != 0 {
		if resp.RetCode == 110026 || strings.Contains(strings.ToLower(resp.RetMsg), "already") {
			return nil
		}
		return fmt.Errorf("bybit set account margin mode error: retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}
	return nil
}

// ChangeLeverage adjusts leverage for a symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	leverageStr := fmt.Sprintf("%d", req.Leverage)
	return c.rawChangeLeverage(ctx, bybitChangeLeverageRequest{
		Category:     categoryLinear,
		Symbol:       req.Symbol,
		BuyLeverage:  leverageStr,
		SellLeverage: leverageStr,
	})
}

// SwitchMarginMode switches the margin mode (CROSS vs ISOLATED) for Bybit.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol, marginMode string, leverage int, side domain.Side) error {
	if strings.EqualFold(c.accountType, "unified") {
		return c.switchUnifiedMarginMode(ctx, marginMode)
	}

	tradeMode := 1 // isolated
	if marginMode == constantCross {
		tradeMode = 0 // cross
	}
	leverageStr := fmt.Sprintf("%d", leverage)

	return c.rawSwitchIsolatedMode(ctx, bybitSwitchIsolatedRequest{
		Category:     categoryLinear,
		Symbol:       symbol,
		TradeMode:    tradeMode,
		BuyLeverage:  leverageStr,
		SellLeverage: leverageStr,
	})
}

func (c *Client) switchUnifiedMarginMode(ctx context.Context, marginMode string) error {
	utaMarginMode := utaMarginIsolated
	if marginMode == constantCross {
		utaMarginMode = utaMarginRegular
	}
	return c.rawSetMarginMode(ctx, bybitSetMarginModeRequest{
		SetMarginMode: utaMarginMode,
	})
}
