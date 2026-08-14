package orders

import (
	"context"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/pkg/decmath"
)

// OrderResult holds the result of an order attempt.
type OrderResult struct {
	Candidate        domain.Candidate
	Order            *exchange.OrderInfo
	OrderID          string
	TPSLSubmitted    bool
	ExternalID       string
	Price            float64
	TakeProfitPrice  float64
	StopLossPrice    float64
	Volume           float64
	FireIOCTime      time.Time
	LocalFireIOCTime time.Time
	Filled           bool
	Error            error
}

func (r *OrderResult) IsSuccess() bool {
	return r.OrderID != "" && r.Error == nil
}

func FireIOC(ctx context.Context, client exchange.Client, candidate *domain.Candidate, ts shared.Clock, logger *slog.Logger) OrderResult {
	log := logger
	extOID := candidate.ExternalID

	iocPrice, err := candidate.CalculateIOCPrice()
	if err != nil {
		log.ErrorContext(ctx, "🔴 IOC calc failed at FireIOC", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return OrderResult{Candidate: *candidate, Error: err}
	}

	tpPrice, slPrice := candidate.CalculateOrderTPSL(ctx, iocPrice, log)

	req := exchange.SubmitOrderRequest{
		Symbol:          candidate.Symbol,
		Price:           iocPrice,
		Vol:             candidate.Volume,
		Side:            candidate.Side,
		Type:            exchange.OrderTypeIOC,
		OpenType:        shared.OpenType(candidate.Config.ParsedOpenType),
		PositionMode:    shared.PositionMode(candidate.Config.ParsedPositionMode),
		Leverage:        candidate.Config.Leverage,
		ExternalOID:     extOID,
		TakeProfitPrice: tpPrice,
		StopLossPrice:   slPrice,
	}

	spread := decmath.CalcSpreadPct(candidate.BestBid, candidate.BestAsk)
	actualNotional := candidate.NotionalForVolume(candidate.Volume, iocPrice)
	log.InfoContext(ctx, "🎯 FIRE IOC",
		slog.String("symbol", candidate.Symbol),
		slog.Float64("price", iocPrice),
		slog.Float64("vol", candidate.Volume),
		slog.Float64("actualNotionalUSDT", actualNotional),
		slog.String("side", candidate.Side.String()),
		slog.Float64("bestBid", candidate.BestBid),
		slog.Float64("bestAsk", candidate.BestAsk),
		slog.Float64("spreadPct", spread),
		slog.Float64("takeProfitPrice", tpPrice),
		slog.String("extOid", extOID),
		slog.Time("syncTime", time.UnixMilli(ts.GetServerTime())),
		slog.Int64("offset_ms", ts.Offset()),
	)

	fireIOCTime := ts.Now()
	localFireIOCTime := time.Now()
	res, err := client.CreateOrder(ctx, req)
	result := OrderResult{
		Candidate:        *candidate,
		OrderID:          res.OrderID,
		TPSLSubmitted:    res.TPSLSubmitted,
		ExternalID:       extOID,
		Price:            iocPrice,
		TakeProfitPrice:  tpPrice,
		StopLossPrice:    slPrice,
		Volume:           candidate.Volume,
		FireIOCTime:      fireIOCTime,
		LocalFireIOCTime: localFireIOCTime,
		Error:            err,
	}
	if err != nil {
		log.ErrorContext(ctx, "🔴 IOC order failed", slog.Any("error", err), slog.String("symbol", candidate.Symbol))
		return result
	}

	log.InfoContext(ctx, "📨 IOC submitted", slog.String("symbol", candidate.Symbol), slog.String("orderID", res.OrderID))
	return result
}
