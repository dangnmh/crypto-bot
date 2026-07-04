package pionex

import (
	"context"
	"fmt"
	"strconv"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type pionexBaseResponse struct {
	Result    bool         `json:"result"`
	Timestamp xjson.Number `json:"timestamp"`
}

type pionexChangeLeverageRequest struct {
	Symbol   string `json:"symbol"`
	Leverage string `json:"leverage"`
}

func (c *Client) rawChangeLeverage(ctx context.Context, symbol string, leverage int) ([]byte, error) {
	reqBody := pionexChangeLeverageRequest{
		Symbol:   symbol,
		Leverage: strconv.Itoa(leverage),
	}
	body, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "POST", "/uapi/v1/account/leverage", nil, body)
}

// ChangeLeverage modifies leverage for a specific symbol.
func (c *Client) ChangeLeverage(ctx context.Context, req exchange.ChangeLeverageRequest) error {
	body, err := c.rawChangeLeverage(ctx, req.Symbol, req.Leverage)
	if err != nil {
		return err
	}
	var resp pionexBaseResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unmarshal pionex change leverage: %w", err)
	}
	if !resp.Result {
		return fmt.Errorf("pionex change leverage failed")
	}
	return nil
}

type pionexSwitchPositionModeRequest struct {
	PositionMode string `json:"positionMode"`
}

func (c *Client) rawSwitchPositionMode(ctx context.Context, mode string) ([]byte, error) {
	reqBody := pionexSwitchPositionModeRequest{
		PositionMode: mode,
	}
	body, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "POST", "/uapi/v1/account/positionMode", nil, body)
}

// SwitchPositionMode switches position mode (Hedge vs One-Way).
func (c *Client) SwitchPositionMode(ctx context.Context, symbol string, positionMode domain.PositionMode) error {
	var modeStr string
	switch positionMode {
	case domain.PositionModeOneWay:
		modeStr = "BUYSELL"
	case domain.PositionModeHedge:
		modeStr = openCloseMode
	default:
		return fmt.Errorf("unsupported position mode: %v", positionMode)
	}

	body, err := c.rawSwitchPositionMode(ctx, modeStr)
	if err != nil {
		return err
	}
	var resp pionexBaseResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unmarshal pionex switch position mode: %w", err)
	}
	if !resp.Result {
		return fmt.Errorf("pionex switch position mode failed")
	}
	return nil
}

type pionexSwitchMarginModeRequest struct {
	Symbol       string `json:"symbol"`
	IsolatedMode string `json:"isolatedMode"`
}

func (c *Client) rawSwitchMarginMode(ctx context.Context, symbol, mode string) ([]byte, error) {
	reqBody := pionexSwitchMarginModeRequest{
		Symbol:       symbol,
		IsolatedMode: mode,
	}
	body, err := xjson.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	return c.rawRequestPrivate(ctx, "POST", "/uapi/v1/trade/isolatedMode", nil, body)
}

// SwitchMarginMode switches between isolated and cross margin mode for a specific symbol.
func (c *Client) SwitchMarginMode(ctx context.Context, symbol string, marginMode domain.MarginMode, leverage int, side domain.Side) error {
	var modeStr string
	switch marginMode {
	case domain.MarginModeCross:
		modeStr = "CROSS"
	case domain.MarginModeIsolated:
		modeStr = "ISOLATED"
	default:
		return fmt.Errorf("unsupported margin mode: %v", marginMode)
	}

	body, err := c.rawSwitchMarginMode(ctx, symbol, modeStr)
	if err != nil {
		return err
	}
	var resp pionexBaseResponse
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("unmarshal pionex switch margin mode: %w", err)
	}
	if !resp.Result {
		return fmt.Errorf("pionex switch margin mode failed")
	}
	return nil
}
