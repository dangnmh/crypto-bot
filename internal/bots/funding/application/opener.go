package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/domain"
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
		logger.Error("🔴 IOC calc failed at FireIOC", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
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

	// MEXC validates TP/SL against the submitted order price, not BestBid/BestAsk.
	// When slippage pushes IOC price beyond the wall-based TP, TP lands on the wrong
	// side → code 5003 "The price of stop-limit order error". Zero out invalid prices.
	if candidate.Side == shared.SideOpenLong {
		if tpPrice > 0 && tpPrice <= iocPrice {
			logger.Warn("🟡 TP below IOC price (LONG), dropping TP",
				slog.Float64("tp", tpPrice), slog.Float64("ioc", iocPrice))
			tpPrice = 0
		}
		if slPrice > 0 && slPrice >= iocPrice {
			logger.Warn("🟡 SL above IOC price (LONG), dropping SL",
				slog.Float64("sl", slPrice), slog.Float64("ioc", iocPrice))
			slPrice = 0
		}
	} else {
		if tpPrice > 0 && tpPrice >= iocPrice {
			logger.Warn("🟡 TP above IOC price (SHORT), dropping TP",
				slog.Float64("tp", tpPrice), slog.Float64("ioc", iocPrice))
			tpPrice = 0
		}
		if slPrice > 0 && slPrice <= iocPrice {
			logger.Warn("🟡 SL below IOC price (SHORT), dropping SL",
				slog.Float64("sl", slPrice), slog.Float64("ioc", iocPrice))
			slPrice = 0
		}
	}

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

	spread := calcSpreadPct(candidate.BestBid, candidate.BestAsk)

	logger.Info("🎯 FIRE IOC",
		slog.String("symbol", candidate.Symbol),
		slog.Float64("price", iocPrice),
		slog.Float64("vol", candidate.Volume),
		slog.String("side", candidate.Side.String()),
		slog.Float64("bestBid", candidate.BestBid),
		slog.Float64("bestAsk", candidate.BestAsk),
		slog.Float64("spreadPct", spread),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.String("extOid", extOID),
		slog.Time("syncTime", time.UnixMilli(ts.GetServerTime())),
		slog.Int64("offset_ms", ts.Offset()),
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 IOC order failed", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: *candidate, Error: err}
	}

	logger.Info("📨 IOC submitted", slog.String("symbol", candidate.Symbol), slog.String("orderID", orderID))
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
		logger.Warn("🟡 Trap price invalid, skipping", slog.String("symbol", candidate.Symbol))
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

	logger.Info("🩤 FIRE TRAP",
		slog.String("symbol", candidate.Symbol),
		slog.Float64("peakPrice", candidate.GetPeakPrice()),
		slog.Float64("trapPrice", trapPrice),
		slog.Float64("vol", candidate.Volume),
		slog.String("trapSide", trapSide.String()),
		slog.String("extOid", extOID),
	)

	orderID, err := client.CreateOrder(ctx, req)
	if err != nil {
		logger.Error("🔴 TRAP order failed", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: *candidate, Error: err}
	}

	logger.Info("📨 TRAP submitted", slog.String("symbol", candidate.Symbol), slog.String("orderID", orderID))
	return OrderResult{Candidate: trapCandidate, OrderID: orderID}
}
