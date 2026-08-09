package weex

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
)

type weexPosition struct {
	Symbol     string `json:"symbol"`
	Side       string `json:"side"` // LONG / SHORT
	Size       string `json:"size"`
	Leverage   string `json:"leverage"`
	OpenPrice  string `json:"openPrice"`
	AvgPrice   string `json:"avgPrice"`
	EntryPrice string `json:"entryPrice"`
}

func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	resBytes, err := c.request(ctx, http.MethodGet, "/capi/v3/account/position/allPosition", nil, nil, true)
	if err != nil {
		return nil, err
	}

	rawList, err := parseResponse[[]weexPosition](resBytes)
	if err != nil {
		return nil, err
	}

	var positions []exchange.Position
	for i := range rawList {
		raw := &rawList[i]
		if symbol != "" && !strings.EqualFold(raw.Symbol, symbol) {
			continue
		}

		vol, _ := strconv.ParseFloat(raw.Size, 64)
		if vol <= 0 {
			continue
		}

		pType := exchange.PositionTypeLong
		if strings.EqualFold(raw.Side, "SHORT") {
			pType = exchange.PositionTypeShort
		}

		avgPrice := 0.0
		switch {
		case raw.EntryPrice != "":
			avgPrice, _ = strconv.ParseFloat(raw.EntryPrice, 64)
		case raw.AvgPrice != "":
			avgPrice, _ = strconv.ParseFloat(raw.AvgPrice, 64)
		case raw.OpenPrice != "":
			avgPrice, _ = strconv.ParseFloat(raw.OpenPrice, 64)
		}

		levVal, _ := strconv.Atoi(raw.Leverage)

		positions = append(positions, exchange.Position{
			Symbol:          strings.ToUpper(raw.Symbol),
			HoldVolContract: vol,
			RawHoldVol:      vol,
			PositionType:    pType,
			OpenAvgPrice:    avgPrice,
			HoldAvgPrice:    avgPrice,
			Leverage:        levVal,
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
		ExternalOID:  exchange.ExternalOrderID(symbol, time.Now(), "weex"),
		Leverage:     leverage,
	})
	return err
}

type weexClosePositionResult struct {
	PositionID     int64  `json:"positionId"`
	Success        bool   `json:"success"`
	SuccessOrderID int64  `json:"successOrderId"`
	ErrorMessage   string `json:"errorMessage"`
}

func (c *Client) CloseAllPositions(ctx context.Context, symbol string) error {
	body := map[string]any{}
	if symbol != "" {
		body[keySymbol] = symbol
	}
	resBytes, err := c.request(ctx, http.MethodPost, "/capi/v3/closePositions", nil, body, true)
	if err != nil {
		return err
	}
	results, err := parseResponse[[]weexClosePositionResult](resBytes)
	if err != nil {
		return err
	}
	for i := range results {
		if !results[i].Success && results[i].ErrorMessage != "" {
			return fmt.Errorf("WEEX close position %d failed: %s", results[i].PositionID, results[i].ErrorMessage)
		}
	}
	return nil
}
