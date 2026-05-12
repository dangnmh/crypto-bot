package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
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
func FireIOC(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger, ob *shared.OrderBook) OrderResult {
	candidate.Phase = domain.PhaseFiredIOC
	extOID := fmt.Sprintf("ioc_%s_%d", candidate.Symbol, time.Now().UnixMilli())

	iocPrice, err := candidate.CalculateIOCPrice(ob)
	if err != nil {
		logger.Error("🔴 IOC calc failed at FireIOC", "error", err, "symbol", candidate.Symbol)
		return OrderResult{Candidate: *candidate, Error: err}
	}

	// Compute server-side Take Profit from OB wall detection.
	// TakeProfitPct is stored as a ratio (e.g. 0.005 = 0.5%), convert to % for calculator.
	var tpPrice float64
	maxTPPct := decmath.Mul(candidate.Config.FundingReversion.TakeProfitPct, 100.0)
	if maxTPPct > 0 {
		tpPrice = candidate.CalculateTakeProfitPrice(ob, maxTPPct)
	}

	// Compute server-side Stop Loss.
	slPrice := candidate.CalculateStopLossPrice(candidate.GetPeakPrice())

	req := exchange.SubmitOrderRequest{
		Symbol:          candidate.Symbol,
		Price:           iocPrice,
		Vol:             candidate.Volume,
		Side:            int(candidate.Side),
		Type:            exchange.OrderTypeIOC,
		OpenType:        candidate.Config.ParsedOpenType,
		PositionMode:    candidate.Config.ParsedPositionMode,
		Leverage:        candidate.Config.Leverage,
		ExternalOID:     extOID,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	spread := 0.0
	if candidate.BestBid > 0 {
		spread = decmath.Mul(decmath.Div(decmath.Sub(candidate.BestAsk, candidate.BestBid), candidate.BestBid), 100.0)
	}

	logger.Info("🎯 FIRE IOC",
		"symbol", candidate.Symbol,
		"price", iocPrice,
		"vol", candidate.Volume,
		"side", candidate.Side.String(),
		"bestBid", candidate.BestBid,
		"bestAsk", candidate.BestAsk,
		"spreadPct", spread,
		"takeProfitPrice", tpPrice,
		"extOid", extOID,
		"syncTime", time.UnixMilli(ts.GetServerTime()),
		"offset_ms", ts.Offset(),
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 IOC order failed", "error", err, "symbol", candidate.Symbol)
		return OrderResult{Candidate: *candidate, Error: err}
	}

	logger.Info("📨 IOC submitted", "symbol", candidate.Symbol, "orderID", orderID)
	return OrderResult{Candidate: *candidate, OrderID: orderID}
}

// FireLimitTrap sends a Maker POST-ONLY order to catch the dump.
func FireLimitTrap(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger) OrderResult {
	candidate.Phase = domain.PhaseFiredTrap
	extOID := fmt.Sprintf("trp_%s_%d", candidate.Symbol, time.Now().UnixMilli())

	// Calculate Trap price cleanly with proper exchange tick/scale snapping.
	// MUST BE CALCULATED BEFORE INVERTING candidate.Side.
	trapPrice := candidate.CalculateTrapPrice()

	// Skip trap if price calculation returned invalid (e.g. depth too extreme or wrong side).
	if trapPrice <= 0 {
		logger.Warn("🟡 Trap price invalid, skipping", "symbol", candidate.Symbol)
		return OrderResult{Candidate: *candidate, Error: fmt.Errorf("trap price <= 0")}
	}

	// If main sniper side is SHORT, trap side should be LONG, and vice-versa.
	trapSide := shared.SideOpenLong
	trapCloseSide := shared.SideCloseLong
	if candidate.Side == shared.SideOpenLong {
		trapSide = shared.SideOpenShort
		trapCloseSide = shared.SideCloseShort
	}
	trapCandidate := *candidate
	trapCandidate.Side = trapSide
	trapCandidate.CloseSide = trapCloseSide

	req := exchange.SubmitOrderRequest{
		Symbol:       candidate.Symbol,
		Price:        trapPrice,
		Vol:          candidate.Volume,
		Side:         int(trapSide),
		Type:         exchange.OrderTypeLimit,
		OpenType:     candidate.Config.ParsedOpenType,
		PositionMode: candidate.Config.ParsedPositionMode,
		Leverage:     candidate.Config.Leverage,
		ExternalOID:  extOID,
	}

	logger.Info("🪤 FIRE TRAP",
		"symbol", candidate.Symbol,
		"peakPrice", candidate.GetPeakPrice(),
		"trapPrice", trapPrice,
		"vol", candidate.Volume,
		"trapSide", trapSide.String(),
		"extOid", extOID,
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 TRAP order failed", "error", err, "symbol", candidate.Symbol)
		return OrderResult{Candidate: *candidate, Error: err}
	}

	logger.Info("📨 TRAP submitted", "symbol", candidate.Symbol, "orderID", orderID)
	return OrderResult{Candidate: trapCandidate, OrderID: orderID}
}
