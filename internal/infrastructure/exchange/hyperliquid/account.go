package hyperliquid

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"crypto-bot/internal/infrastructure/exchange"

	"github.com/samber/lo"
	hl "github.com/sonirico/go-hyperliquid"
)

const exchangeName = "hyperliquid"

type hyperliquidUserStateRequest struct {
	UserAddress string
}

type hyperliquidQueryOrderByCloidRequest struct {
	UserAddress string
	Cloid       string
}

type hyperliquidUserFillsRequest struct {
	UserAddress string
}

// Private raw methods invoking the Hyperliquid API or SDK.

func (c *Client) getRawUserState(ctx context.Context, req hyperliquidUserStateRequest) (*hl.UserState, error) {
	return c.info.UserState(ctx, req.UserAddress)
}

func (c *Client) getRawOrderByCloid(ctx context.Context, req hyperliquidQueryOrderByCloidRequest) (*hl.OrderQueryResult, error) {
	return c.info.QueryOrderByCloid(ctx, req.UserAddress, req.Cloid)
}

func (c *Client) getRawUserFills(ctx context.Context, req hyperliquidUserFillsRequest) ([]hl.Fill, error) {
	params := hl.UserFillsParams{
		Address: req.UserAddress,
	}
	return c.info.UserFills(ctx, params)
}

// Public mapper methods implementing the exchange.AccountDataProvider interface.

// GetOpenPositions returns all currently open perp positions.
func (c *Client) GetOpenPositions(ctx context.Context, symbol string) ([]exchange.Position, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	state, err := c.getRawUserState(ctx, hyperliquidUserStateRequest{
		UserAddress: c.userAddress,
	})
	if err != nil {
		return nil, err
	}

	positions := make([]exchange.Position, 0, len(state.AssetPositions))
	for i := range state.AssetPositions {
		p := &state.AssetPositions[i].Position
		if symbol != "" && p.Coin != symbol {
			continue
		}

		szi, _ := strconv.ParseFloat(p.Szi, 64)
		if szi == 0 {
			continue
		}

		entryPrice, _ := strconv.ParseFloat(lo.FromPtr(p.EntryPx), 64)

		posSide := exchange.PositionTypeLong // Long.
		if szi < 0 {
			posSide = exchange.PositionTypeShort // Short.
		}

		positions = append(positions, exchange.Position{
			Symbol:       p.Coin,
			HoldVolCoin:  math.Abs(szi),
			RawHoldVol:   math.Abs(szi),
			HoldAvgPrice: entryPrice,
			OpenAvgPrice: entryPrice,
			PositionType: posSide,
		})
	}
	return positions, nil
}

func (c *Client) GetOrderPNL(ctx context.Context, symbol, orderID string) (*exchange.ClosedPnLInfo, error) {
	orderInfo, err := c.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get order by ID %s failed: %w", orderID, err)
	}
	if orderInfo.State == exchange.OrderStateCanceled {
		return &exchange.ClosedPnLInfo{
			Exchange: exchangeName,
			Symbol:   symbol,
		}, nil
	}

	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	// Parse numeric orderID.
	closingOrderId, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid orderID format: %w", err)
	}

	var startTime time.Time
	if orderInfo.CreateTime > 0 {
		startTime = time.UnixMilli(orderInfo.CreateTime - 1000)
	}

	fills, err := c.getRawUserFills(ctx, hyperliquidUserFillsRequest{
		UserAddress: c.userAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get user fills: %w", err)
	}

	symFills := filterHyperliquidFills(fills, symbol, startTime)
	if len(symFills) == 0 {
		return nil, fmt.Errorf("no user fills found for symbol %s", symbol)
	}

	latestFill := findLatestHyperliquidFill(symFills)
	agg := aggregateHyperliquidFills(symFills, closingOrderId)

	if agg.totalQty == 0 {
		return nil, fmt.Errorf("zero quantity for closing order %d", closingOrderId)
	}

	exitPrice := agg.weightedPriceSum / agg.totalQty
	var entryPrice float64

	// If closing side was Sell ("S"), the position was Long.
	isLong := strings.EqualFold(latestFill.Side, "S")
	if isLong {
		entryPrice = exitPrice - (agg.totalRealizedPnl / agg.totalQty)
	} else {
		entryPrice = exitPrice + (agg.totalRealizedPnl / agg.totalQty)
	}

	return &exchange.ClosedPnLInfo{
		Exchange:       exchangeName,
		Symbol:         latestFill.Coin,
		EntryPrice:     entryPrice,
		ExitPrice:      exitPrice,
		ClosedSizeCoin: new(agg.totalQty),
		GrossPnL:       agg.totalRealizedPnl,
		Fee:            agg.totalCommission,
		FundingFee:     0,
		DurationMs:     0,
	}, nil
}

func filterHyperliquidFills(fills []hl.Fill, symbol string, startTime time.Time) []hl.Fill {
	var symFills []hl.Fill
	for i := range fills {
		f := &fills[i]
		if strings.EqualFold(f.Coin, symbol) {
			if !startTime.IsZero() && f.Time < startTime.UnixMilli() {
				continue
			}
			symFills = append(symFills, *f)
		}
	}
	return symFills
}

func findLatestHyperliquidFill(fills []hl.Fill) *hl.Fill {
	if len(fills) == 0 {
		return nil
	}
	latestFill := &fills[0]
	for i := range fills {
		f := &fills[i]
		if f.Time > latestFill.Time {
			latestFill = f
		}
	}
	return latestFill
}

type hlAggResult struct {
	totalQty         float64
	totalRealizedPnl float64
	totalCommission  float64
	weightedPriceSum float64
}

func aggregateHyperliquidFills(fills []hl.Fill, closingOrderId int64) hlAggResult {
	var res hlAggResult
	for i := range fills {
		item := &fills[i]
		if item.Oid != closingOrderId {
			continue
		}
		qty, _ := strconv.ParseFloat(item.Size, 64)
		price, _ := strconv.ParseFloat(item.Price, 64)
		realizedPnl, _ := strconv.ParseFloat(item.ClosedPnl, 64)
		commission, _ := strconv.ParseFloat(item.Fee, 64)

		res.totalQty += qty
		res.totalRealizedPnl += realizedPnl
		res.totalCommission += commission
		res.weightedPriceSum += price * qty
	}
	return res
}
