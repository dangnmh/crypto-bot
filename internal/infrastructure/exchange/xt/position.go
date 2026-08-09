package xt

import (
	"context"
	"fmt"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type xtOpenPosition struct {
	Symbol       string       `json:"symbol"`
	PositionSide string       `json:"positionSide"` // LONG, SHORT
	PositionSize xjson.Number `json:"positionSize"`
	EntryPrice   xjson.Number `json:"entryPrice"`
	Leverage     int          `json:"leverage"`
}

type xtPositionListResponse struct {
	ReturnCode int64            `json:"returnCode"`
	MsgInfo    string           `json:"msgInfo"`
	Result     []xtOpenPosition `json:"result"`
}

type xtHistoryPosition struct {
	ID                string       `json:"id"`
	PositionSide      string       `json:"positionSide"`
	ContractType      string       `json:"contractType"`
	Symbol            string       `json:"symbol"`
	PositionType      int          `json:"positionType"`
	CloseProfit       xjson.Number `json:"closeProfit"`
	ClosePositionSize xjson.Number `json:"closePositionSize"`
	CloseOpenPrice    xjson.Number `json:"closeOpenPrice"`
	ClosePrice        xjson.Number `json:"closePrice"`
	MaxPositionSize   xjson.Number `json:"maxPositionSize"`
	OpenTime          int64        `json:"openTime"`
	CloseTime         int64        `json:"closeTime"`
	StartLeverage     int          `json:"startLeverage"`
	EndLeverage       int          `json:"endLeverage"`
	Working           bool         `json:"working"`
	Force             bool         `json:"force"`
	ForceMarkPrice    *string      `json:"forceMarkPrice"`
	TotalFee          xjson.Number `json:"totalFee"`
	TotalFundFee      xjson.Number `json:"totalFundFee"`
	WelfareAccount    bool         `json:"welfareAccount"`
}

type xtPositionHistoryResult struct {
	HasNext bool                `json:"hasNext"`
	HasPrev bool                `json:"hasPrev"`
	Items   []xtHistoryPosition `json:"items"`
}

type xtPositionHistoryResponse struct {
	ReturnCode int64                   `json:"returnCode"`
	MsgInfo    string                  `json:"msgInfo"`
	Result     xtPositionHistoryResult `json:"result"`
}

func (c *Client) rawGetOpenPositions(ctx context.Context, symbol string) ([]xtOpenPosition, error) {
	query := make(map[string]string)
	if symbol != "" {
		sym := cleanXTSymbol(symbol)
		query["symbol"] = sym
	}

	respBytes, err := c.request(ctx, "GET", "/future/user/v1/position", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp xtPositionListResponse
	if err := xjson.Unmarshal(respBytes, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal position list: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("get positions API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return resp.Result, nil
}

func (c *Client) rawGetHistoryPositions(ctx context.Context, params map[string]string) ([]xtHistoryPosition, error) {
	respBytes, err := c.GetHistoryPositionsRaw(ctx, params)
	if err != nil {
		return nil, err
	}

	var resp xtPositionHistoryResponse
	err = xjson.Unmarshal(respBytes, &resp)
	if err != nil {
		return nil, fmt.Errorf("unmarshal history position response: %w", err)
	}

	if resp.ReturnCode != 0 {
		return nil, fmt.Errorf("get history positions API error: code=%d, msg=%s", resp.ReturnCode, resp.MsgInfo)
	}

	return resp.Result.Items, nil
}

// GetOpenPositions satisfies the Client interface.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	raw, err := c.rawGetOpenPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	var results []exchange.Position
	for _, p := range raw {
		hv, _ := p.PositionSize.Float64()
		if hv == 0 {
			continue
		}

		posType := exchange.PositionTypeLong
		if strings.EqualFold(p.PositionSide, sideShort) {
			posType = exchange.PositionTypeShort
		}
		ep, _ := p.EntryPrice.Float64()

		results = append(results, exchange.Position{
			Symbol:          toStandardSymbol(p.Symbol),
			HoldVolContract: hv,
			RawHoldVol:      hv,
			PositionType:    posType,
			OpenAvgPrice:    ep,
			HoldAvgPrice:    ep,
			Leverage:        p.Leverage,
		})
	}

	return results, nil
}

// ClosePosition satisfies the Client interface.
func (c *Client) ClosePosition(
	ctx context.Context,
	symbol string,
	closeSide domain.Side,
	volume float64,
	positionMode domain.PositionMode,
	leverage int,
) error {
	raw, err := c.rawGetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	targetSide := sideLong
	if closeSide == domain.SideCloseShort {
		targetSide = sideShort
	}

	var targetSize float64
	for _, p := range raw {
		if toStandardSymbol(p.Symbol) == toStandardSymbol(symbol) && strings.EqualFold(p.PositionSide, targetSide) {
			targetSize, _ = p.PositionSize.Float64()
			break
		}
	}

	if targetSize == 0 {
		// Already closed
		return nil
	}

	qtyToClose := targetSize
	if volume > 0 && volume < targetSize {
		qtyToClose = volume
	}

	// Place MARKET close order
	submitReq := exchange.SubmitOrderRequest{
		Symbol:     symbol,
		Vol:        qtyToClose,
		Side:       closeSide,
		Type:       domain.OrderTypeMarket,
		ReduceOnly: true,
	}

	_, err = c.CreateOrder(ctx, submitReq)
	return err
}

// CloseAllPositions satisfies the Client interface.
func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	// Cancel all open orders first
	_ = c.CancelAllOpenOrders(ctx, symbol)

	// Fetch and close any remaining positions
	openPos, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for _, p := range openPos {
		var closeSide domain.Side
		if p.PositionType == exchange.PositionTypeLong {
			closeSide = domain.SideCloseLong
		} else {
			closeSide = domain.SideCloseShort
		}
		vol := p.HoldVolContract
		if vol == 0 {
			vol = p.HoldVolCoin
		}
		_ = c.ClosePosition(ctx, symbol, closeSide, vol, domain.PositionModeHedge, p.Leverage)
	}

	return nil
}
