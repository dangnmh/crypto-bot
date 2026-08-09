package orangex

import (
	"context"
	"math"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type positionResult struct {
	InstrumentName string       `json:"instrument_name"`
	Direction      string       `json:"direction"`
	Size           xjson.Number `json:"size"`
	AveragePrice   xjson.Number `json:"average_price"`
	Leverage       xjson.Number `json:"leverage"`
	Margin         xjson.Number `json:"margin"`
}

func (c *Client) rawGetPositions(ctx context.Context, currency, kind string) ([]positionResult, error) {
	params := map[string]string{
		paramCurrency: currency,
	}
	if kind != "" {
		params["kind"] = kind
	}
	resp, err := c.postRPC(ctx, "/private/get_positions", "/private/get_positions", params, true)
	if err != nil {
		return nil, err
	}
	var envelope orangexRPCResponse[[]positionResult]
	if err := xjson.Unmarshal(resp, &envelope); err != nil {
		return nil, err
	}
	if envelope.Error != nil {
		return nil, envelope.Error
	}
	return envelope.Result, nil
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	res, err := c.rawGetPositions(ctx, "PERPETUAL", "perpetual")
	if err != nil {
		return nil, err
	}
	var out []exchange.Position
	for _, p := range res {
		if symbol != "" && p.InstrumentName != symbol {
			continue
		}
		sizeVal := math.Abs(xjson.ToFloat64(p.Size))
		if sizeVal == 0 {
			continue
		}
		pType := exchange.PositionTypeLong
		if p.Direction == dirSell {
			pType = exchange.PositionTypeShort
		}
		out = append(out, exchange.Position{
			Symbol:          p.InstrumentName,
			PositionType:    pType,
			OpenAvgPrice:    xjson.ToFloat64(p.AveragePrice),
			HoldAvgPrice:    xjson.ToFloat64(p.AveragePrice),
			HoldVolContract: sizeVal,
			RawHoldVol:      sizeVal,
			Leverage:        int(xjson.ToFloat64(p.Leverage)),
		})
	}
	return out, nil
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	params := map[string]any{
		paramInstrument: symbol,
		paramType:       typeMarket,
		paramAmount:     volume,
	}
	_, err := c.postRPC(ctx, "/private/close_position", "/private/close_position", params, true)
	return err
}

func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	positions, err := c.GetOpenPositions(ctx, symbol)
	if err != nil {
		return err
	}
	for _, p := range positions {
		var side domain.Side
		if p.PositionType == exchange.PositionTypeLong {
			side = domain.SideCloseLong
		} else {
			side = domain.SideCloseShort
		}
		vol := p.HoldVolContract
		if vol == 0 {
			vol = p.HoldVolCoin
		}
		if err := c.ClosePosition(ctx, p.Symbol, side, vol, domain.PositionModeHedge, 0); err != nil {
			return err
		}
	}
	return nil
}
