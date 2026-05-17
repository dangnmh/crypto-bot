package orders

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
	Candidate       domain.Candidate
	Order           *exchange.OrderInfo
	OrderID         string
	Price           float64
	TakeProfitPrice float64
	StopLossPrice   float64
	Volume          float64
	Filled          bool
	Error           error
}

func (r *OrderResult) IsSuccess() bool {
	return r.OrderID != "" && r.Error == nil
}

func FireIOC(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger, ob *shared.OrderBook) OrderResult {
	extOID := fmt.Sprintf("ioc_%s_%d", candidate.Symbol, time.Now().UnixMilli())

	iocPrice, err := candidate.CalculateIOCPrice(ob)
	if err != nil {
		logger.Error("🔴 IOC calc failed at FireIOC", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: *candidate, Error: err}
	}

	var tpPrice float64
	maxTPPct := decmath.Mul(candidate.Config.FundingReversion.TakeProfitPct, 100.0)
	if maxTPPct > 0 {
		tpPrice = candidate.CalculateTakeProfitPrice(ob, maxTPPct)
	}

	slPrice := candidate.CalculateStopLossPrice(candidate.GetPeakPrice())
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
	result := OrderResult{
		Candidate:       *candidate,
		OrderID:         orderID,
		Price:           iocPrice,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
		Volume:          candidate.Volume,
		Error:           err,
	}
	if err != nil {
		logger.Error("🔴 IOC order failed", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return result
	}

	logger.Info("📨 IOC submitted", slog.String("symbol", candidate.Symbol), slog.String("orderID", orderID))
	return result
}

func FireLimitTrap(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger) OrderResult {
	extOID := fmt.Sprintf("trp_%s_%d", candidate.Symbol, time.Now().UnixMilli())

	trapPrice := candidate.CalculateTrapPrice()
	if trapPrice <= 0 {
		logger.Warn("🟡 Trap price invalid, skipping", slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: *candidate, Error: fmt.Errorf("trap price <= 0")}
	}

	trapSide := shared.SideOpenLong
	trapCloseSide := shared.SideCloseLong
	if candidate.Side == shared.SideOpenLong {
		trapSide = shared.SideOpenShort
		trapCloseSide = shared.SideCloseShort
	}
	trapCandidate := *candidate
	trapCandidate.Side = trapSide
	trapCandidate.CloseSide = trapCloseSide
	trapCandidate.Volume = trapCandidate.CalculateTrapVolume(trapPrice)
	if trapCandidate.Volume <= 0 {
		logger.Warn("🟡 Trap volume invalid, skipping", slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: trapCandidate, Error: fmt.Errorf("trap volume <= 0")}
	}
	tpPrice := trapCandidate.CalculateTrapTPPrice(trapPrice)
	slPrice := trapCandidate.CalculateTrapSLPrice(trapPrice)

	req := exchange.SubmitOrderRequest{
		Symbol:          candidate.Symbol,
		Price:           trapPrice,
		Vol:             trapCandidate.Volume,
		Side:            int(trapSide),
		Type:            exchange.OrderTypeLimit,
		OpenType:        candidate.Config.ParsedOpenType,
		PositionMode:    candidate.Config.ParsedPositionMode,
		Leverage:        candidate.Config.Leverage,
		ExternalOID:     extOID,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	logger.Info("🩤 FIRE TRAP",
		slog.String("symbol", candidate.Symbol),
		slog.Float64("peakPrice", candidate.GetPeakPrice()),
		slog.Float64("trapPrice", trapPrice),
		slog.Float64("vol", trapCandidate.Volume),
		slog.String("trapSide", trapSide.String()),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.Float64("stopLossPrice", slPrice),
		slog.String("extOid", extOID),
	)

	orderID, err := client.CreateOrder(ctx, req)
	result := OrderResult{
		Candidate:       trapCandidate,
		OrderID:         orderID,
		Price:           trapPrice,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
		Volume:          trapCandidate.Volume,
		Error:           err,
	}
	if err != nil {
		logger.Error("🔴 TRAP order failed", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return result
	}

	logger.Info("📨 TRAP submitted", slog.String("symbol", candidate.Symbol), slog.String("orderID", orderID))
	return result
}

func calcSpreadPct(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(decmath.Sub(bestAsk, bestBid), bestBid), 100.0)
}
