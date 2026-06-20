package deepcoin

import (
	"context"
	"strconv"

	"crypto-bot/internal/infrastructure/exchange"
)

type deepcoinPosition struct {
	InstID      string     `json:"instId"`
	PosSide     string     `json:"posSide"`
	Pos         flexString `json:"pos"`
	AvgPx       flexString `json:"avgPx"`
	Lever       flexString `json:"lever"`
	Ccy         string     `json:"ccy"`
	MgnMode     string     `json:"mgnMode"`
	MrgPosition string     `json:"mrgPosition"`
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	if symbol != "" {
		params["instId"] = symbol
	}
	body, err := c.GetCtx(ctx, "/deepcoin/account/positions", params)
	if err != nil {
		return nil, err
	}

	res, err := ParseResponse[deepcoinPosition](body, "positions")
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(res))
	for i := range res {
		p := &res[i]
		vol, _ := strconv.ParseFloat(string(p.Pos), 64)
		if vol == 0 {
			continue
		}
		entry, _ := strconv.ParseFloat(string(p.AvgPx), 64)
		posType := exchange.PositionTypeLong
		if p.PosSide == posSideShort {
			posType = exchange.PositionTypeShort
		}

		positions = append(positions, exchange.Position{
			Symbol:       p.InstID,
			HoldVol:      vol,
			HoldAvgPrice: entry,
			OpenAvgPrice: entry,
			PositionType: posType,
		})
	}
	return positions, nil
}
