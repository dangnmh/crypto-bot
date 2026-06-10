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

// GetAssets retrieves account balances.
func (c *Client) GetAssets(ctx context.Context) ([]exchange.AssetInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	state, err := c.getRawUserState(ctx, hyperliquidUserStateRequest{
		UserAddress: c.userAddress,
	})
	if err != nil {
		return nil, err
	}

	equity, _ := strconv.ParseFloat(state.MarginSummary.AccountValue, 64)
	avail, _ := strconv.ParseFloat(state.Withdrawable, 64)
	marginUsed, _ := strconv.ParseFloat(state.MarginSummary.TotalMarginUsed, 64)

	assets := []exchange.AssetInfo{
		{
			Currency:         settleUsdc,
			Equity:           equity,
			AvailableBalance: avail,
			FrozenBalance:    marginUsed,
			CashBalance:      equity - marginUsed,
			Unrealized:       equity - (avail + marginUsed),
		},
	}
	return assets, nil
}

// GetAssetByCurrency retrieves balance details for a specific currency.
func (c *Client) GetAssetByCurrency(ctx context.Context, currency string) (*exchange.AssetInfo, error) {
	assets, err := c.GetAssets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range assets {
		if assets[i].Currency == currency {
			return &assets[i], nil
		}
	}
	return nil, fmt.Errorf("asset not found: %s", currency)
}

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
			HoldVol:      math.Abs(szi),
			HoldAvgPrice: entryPrice,
			OpenAvgPrice: entryPrice,
			PositionType: posSide,
		})
	}
	return positions, nil
}

// GetRecentClosedPnL queries the recent trade fills from Hyperliquid for a symbol, aggregates closing fills, and returns closed trade metrics.
func (c *Client) GetRecentClosedPnL(ctx context.Context, symbol, extOrderID string, startTime time.Time) (*exchange.ClosedPnLInfo, error) {
	if c.userAddress == "" {
		return nil, fmt.Errorf("user address is missing: L1 key is not configured")
	}

	// Look up numeric orderID from client order ID (extOrderID / cloid).
	orderRes, err := c.getRawOrderByCloid(ctx, hyperliquidQueryOrderByCloidRequest{
		UserAddress: c.userAddress,
		Cloid:       extOrderID,
	})
	if err != nil {
		return nil, fmt.Errorf("hyperliquid query order by cloid %s failed: %w", extOrderID, err)
	}
	closingOrderId := orderRes.Order.Order.Oid

	fills, err := c.getRawUserFills(ctx, hyperliquidUserFillsRequest{
		UserAddress: c.userAddress,
	})
	if err != nil {
		return nil, fmt.Errorf("hyperliquid get user fills: %w", err)
	}

	// Filter by symbol and startTime.
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

	if len(symFills) == 0 {
		return nil, fmt.Errorf("no user fills found for symbol %s", symbol)
	}

	// Find the latest fill by time.
	latestFill := &symFills[0]
	for i := range symFills {
		f := &symFills[i]
		if f.Time > latestFill.Time {
			latestFill = f
		}
	}

	var totalQty float64
	var totalRealizedPnl float64
	var totalCommission float64
	var weightedPriceSum float64

	for i := range symFills {
		item := &symFills[i]
		if item.Oid != closingOrderId {
			continue
		}
		qty, _ := strconv.ParseFloat(item.Size, 64)
		price, _ := strconv.ParseFloat(item.Price, 64)
		realizedPnl, _ := strconv.ParseFloat(item.ClosedPnl, 64)
		commission, _ := strconv.ParseFloat(item.Fee, 64)

		totalQty += qty
		totalRealizedPnl += realizedPnl
		totalCommission += commission
		weightedPriceSum += price * qty
	}

	if totalQty == 0 {
		return nil, fmt.Errorf("zero quantity for closing order %d", closingOrderId)
	}

	exitPrice := weightedPriceSum / totalQty
	var entryPrice float64

	// If closing side was Sell ("S"), the position was Long.
	isLong := strings.EqualFold(latestFill.Side, "S")
	if isLong {
		entryPrice = exitPrice - (totalRealizedPnl / totalQty)
	} else {
		entryPrice = exitPrice + (totalRealizedPnl / totalQty)
	}

	return &exchange.ClosedPnLInfo{
		Symbol:     latestFill.Coin,
		EntryPrice: entryPrice,
		ExitPrice:  exitPrice,
		ClosedSize: totalQty,
		GrossPnL:   totalRealizedPnl,
		Fee:        totalCommission,
		FundingFee: 0,
		DurationMs: 0,
	}, nil
}
