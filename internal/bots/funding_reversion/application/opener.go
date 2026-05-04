package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/timesync"
)

// OrderResult holds the result of an order attempt (IOC or Trap).
type OrderResult struct {
	Candidate domain.Candidate
	Order     *exchange.OrderInfo
	OrderID   string
	Filled    bool
	Error     error
}

// IsSuccess returns true if the order was submitted without error.
func (r *OrderResult) IsSuccess() bool {
	return r.OrderID != "" && r.Error == nil
}

// FireIOC sends a single IOC order for capturing the peak.
func FireIOC(ctx context.Context, client *exchange.Client, coin *domain.Candidate, ts *timesync.TimeSync, logger *slog.Logger, ob *exchange.OrderBook) OrderResult {
	coin.Phase = "FIRED_IOC"
	extOID := fmt.Sprintf("ioc_%s_%d", coin.Symbol, time.Now().UnixMilli())

	iocPrice, err := coin.CalculateIOCPrice(ob)
	if err != nil {
		logger.Error("🔴 IOC calc failed at FireIOC", "error", err, "symbol", coin.Symbol)
		return OrderResult{Candidate: *coin, Error: err}
	}

	req := exchange.SubmitOrderRequest{
		Symbol:       coin.Symbol,
		Price:        iocPrice,
		Vol:          coin.Volume,
		Side:         coin.Side,
		Type:         exchange.OrderTypeIOC,
		OpenType:     coin.Config.ParsedOpenType,
		PositionMode: coin.Config.ParsedPositionMode,
		Leverage:     coin.Config.Leverage,
		ExternalOID:  extOID,
	}

	spread := 0.0
	if coin.BestBid > 0 {
		spread = (coin.BestAsk - coin.BestBid) / coin.BestBid * 100.0
	}

	logger.Info("🎯 FIRE IOC",
		"symbol", coin.Symbol,
		"price", iocPrice,
		"vol", coin.Volume,
		"side", coin.Side,
		"bestBid", coin.BestBid,
		"bestAsk", coin.BestAsk,
		"spreadPct", spread,
		"extOid", extOID,
		"syncTime", time.UnixMilli(ts.GetServerTime()),
		"offset_ms", ts.Offset(),
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 IOC order failed", "error", err, "symbol", coin.Symbol)
		return OrderResult{Candidate: *coin, Error: err}
	}

	logger.Info("📨 IOC submitted", "symbol", coin.Symbol, "orderID", orderID)
	return OrderResult{Candidate: *coin, OrderID: orderID}
}

// FireLimitTrap sends a Maker POST-ONLY order to catch the dump.
func FireLimitTrap(ctx context.Context, client *exchange.Client, coin *domain.Candidate, ts *timesync.TimeSync, logger *slog.Logger) OrderResult {
	coin.Phase = "FIRED_TRAP"
	extOID := fmt.Sprintf("trp_%s_%d", coin.Symbol, time.Now().UnixMilli())

	// Calculate Trap price cleanly with proper exchange tick/scale snapping
	// MUST BE CALCULATED BEFORE INVERTING coin.Side
	trapPrice := coin.CalculateTrapPrice()

	// Skip trap if price calculation returned invalid (e.g. depth too extreme or wrong side)
	if trapPrice <= 0 {
		logger.Warn("🟡 Trap price invalid, skipping", "symbol", coin.Symbol)
		return OrderResult{Candidate: *coin, Error: fmt.Errorf("trap price <= 0")}
	}

	// If main sniper side is SHORT, trap side should be LONG, and vice-versa
	trapSide := exchange.SideOpenLong
	trapCloseSide := exchange.SideCloseLong
	if coin.Side == exchange.SideOpenLong {
		trapSide = exchange.SideOpenShort
		trapCloseSide = exchange.SideCloseShort
	}
	trapCandidate := *coin
	trapCandidate.Side = trapSide
	trapCandidate.CloseSide = trapCloseSide

	req := exchange.SubmitOrderRequest{
		Symbol:       coin.Symbol,
		Price:        trapPrice,
		Vol:          coin.Volume,
		Side:         trapSide,
		Type:         exchange.OrderTypeLimit,
		OpenType:     coin.Config.ParsedOpenType,
		PositionMode: coin.Config.ParsedPositionMode,
		Leverage:     coin.Config.Leverage,
		ExternalOID:  extOID,
	}

	logger.Info("🪤 FIRE TRAP",
		"symbol", coin.Symbol,
		"peakPrice", coin.GetPeakPrice(),
		"trapPrice", trapPrice,
		"vol", coin.Volume,
		"trapSide", trapSide,
		"extOid", extOID,
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 TRAP order failed", "error", err, "symbol", coin.Symbol)
		return OrderResult{Candidate: *coin, Error: err}
	}

	logger.Info("📨 TRAP submitted", "symbol", coin.Symbol, "orderID", orderID)
	return OrderResult{Candidate: trapCandidate, OrderID: orderID}
}
