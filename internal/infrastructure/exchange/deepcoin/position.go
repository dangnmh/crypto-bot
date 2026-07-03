package deepcoin

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/xjson"
)

type deepcoinPosition struct {
	InstID      string       `json:"instId"`
	PosSide     string       `json:"posSide"`
	Pos         xjson.Number `json:"pos"`
	AvgPx       xjson.Number `json:"avgPx"`
	Lever       xjson.Number `json:"lever"`
	Ccy         string       `json:"ccy"`
	MgnMode     string       `json:"mgnMode"`
	MrgPosition string       `json:"mrgPosition"`
}

type deepcoinClosePositionErrorItem struct {
	InstID        string `json:"instId"`
	PosiDirection string `json:"posiDirection"`
	ErrorCode     int    `json:"errorCode"`
	ErrorMsg      string `json:"errorMsg"`
}

type deepcoinBatchCloseResultData struct {
	ErrorList []deepcoinClosePositionErrorItem `json:"errorList"`
}

// Private raw methods.

func (c *Client) rawGetOpenPositions(ctx context.Context, params map[string]string) ([]deepcoinPosition, error) {
	body, err := c.GetCtx(ctx, "/deepcoin/account/positions", params)
	if err != nil {
		return nil, err
	}
	return ParseResponse[deepcoinPosition](body, "positions")
}

func (c *Client) rawBatchClosePosition(ctx context.Context, symbol string) (*deepcoinBatchCloseResultData, error) {
	req := map[string]string{
		"productGroup": instTypeSwapU,
		"instId":       symbol,
	}
	body, err := c.PostCtx(ctx, "/deepcoin/trade/batch-close-position", req)
	if err != nil {
		return nil, err
	}
	res, err := ParseResponseFirst[deepcoinBatchCloseResultData](body, "batch_close_position")
	if err != nil {
		return nil, err
	}
	return &res, nil
}

// Public AccountProvider methods.

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	params := map[string]string{
		paramInstType: instTypeSwap,
	}
	if symbol != "" {
		params["instId"] = symbol
	}

	res, err := c.rawGetOpenPositions(ctx, params)
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

		levVal, _ := p.Lever.Int64()

		positions = append(positions, exchange.Position{
			Symbol:       p.InstID,
			HoldVol:      vol,
			HoldAvgPrice: entry,
			OpenAvgPrice: entry,
			PositionType: posType,
			Leverage:     int(levVal),
		})
	}
	return positions, nil
}

func (c *Client) ClosePosition(ctx context.Context, symbol string, closeSide domain.Side, volume float64, positionMode domain.PositionMode, leverage int) error {
	submitSide := exchange.SideCloseLong
	if closeSide == domain.SideCloseShort {
		submitSide = exchange.SideCloseShort
	}
	_, err := c.CreateOrder(ctx, exchange.SubmitOrderRequest{
		Symbol:       symbol,
		Side:         submitSide,
		Type:         exchange.OrderTypeMarket,
		Vol:          volume,
		PositionMode: positionMode,
		Leverage:     leverage,
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "deepcoin"),
	})
	return err
}

func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	res, err := c.rawBatchClosePosition(ctx, symbol)
	if err != nil {
		return fmt.Errorf("deepcoin batch close position: %w", err)
	}
	if len(res.ErrorList) > 0 {
		var errMsgs []string
		for _, item := range res.ErrorList {
			errMsgs = append(errMsgs, fmt.Sprintf("failed to close %s position for %s: %s (code %d)", item.PosiDirection, item.InstID, item.ErrorMsg, item.ErrorCode))
		}
		return fmt.Errorf("batch close failed: %s", strings.Join(errMsgs, "; "))
	}
	return nil
}
