package aster

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type asterPosition struct {
	Symbol           string `json:"symbol"`
	PositionAmt      string `json:"positionAmt"`
	EntryPrice       string `json:"entryPrice"`
	UnrealizedProfit string `json:"unrealizedProfit"`
	PositionSide     string `json:"positionSide"`
	Leverage         string `json:"leverage"`
	MarginType       string `json:"marginType"`
}

func (c *Client) rawGetPositionRisk(ctx context.Context, symbol string) ([]asterPosition, error) {
	params := make(map[string]string)
	if symbol != "" {
		params[paramSymbol] = symbol
	}
	body, err := c.request(ctx, http.MethodGet, "/fapi/v3/positionRisk", params, true)
	if err != nil {
		return nil, err
	}
	var resp []asterPosition
	if err := xjson.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	rawPos, err := c.rawGetPositionRisk(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(rawPos))
	for i := range rawPos {
		p := &rawPos[i]
		size, _ := strconv.ParseFloat(p.PositionAmt, 64)
		entry, _ := strconv.ParseFloat(p.EntryPrice, 64)
		lev, _ := strconv.Atoi(p.Leverage)

		if size == 0 {
			continue
		}

		pType := exchange.PositionTypeLong
		if size < 0 {
			pType = exchange.PositionTypeShort
			size = -size
		}

		positions = append(positions, exchange.Position{
			Symbol:          strings.ToUpper(p.Symbol),
			HoldVolContract: size,
			RawHoldVol:      size,
			HoldAvgPrice:    entry,
			OpenAvgPrice:    entry,
			PositionType:    pType,
			Leverage:        lev,
		})
	}
	return positions, nil
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	sideStr := sideSell
	posSideStr := posSideLong

	if closeSide == domain.SideCloseShort {
		sideStr = sideBuy
		posSideStr = posSideShort
	}

	params := map[string]string{
		paramSymbol:   symbol,
		paramSide:     sideStr,
		paramType:     typeMarket,
		paramQuantity: strconv.FormatFloat(volume, 'f', -1, 64),
	}

	if positionMode == domain.PositionModeHedge {
		params[paramPositionSide] = posSideStr
	} else {
		params[paramPositionSide] = posSideBoth
		params[paramReduceOnly] = valTrue
	}

	_, err := c.rawCreateOrder(ctx, params)
	return err
}

func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}

	for i := range positions {
		p := &positions[i]
		side := domain.SideCloseLong
		if p.PositionType == exchange.PositionTypeShort {
			side = domain.SideCloseShort
		}

		vol := p.HoldVolContract
		if vol == 0 {
			vol = p.HoldVolCoin
		}
		err := c.ClosePosition(ctx, symbol, side, vol, domain.PositionModeHedge, p.Leverage)
		if err != nil {
			return fmt.Errorf("failed to close position: %w", err)
		}
	}

	return nil
}
