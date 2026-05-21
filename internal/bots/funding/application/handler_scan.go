package application

import (
	"context"
	"log/slog"
	"math"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

func (o *CycleOrchestrator) subscribeScan(ctx context.Context) {
	o.rt.Subscribe(ctx, events.TopicScanStart, func(_ *message.Message) {
		o.handleScan(ctx)
	})
}

func (o *CycleOrchestrator) handleScan(ctx context.Context) {
	cfg := o.rt.Config()
	td, err := o.rt.Deps().TickerStore.GetTicker(ctx, cfg.Symbol)
	if err != nil {
		o.rt.Log().Warn("No ticker", slog.Any("error", err))
		o.abort("scan", "no ticker data")
		return
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		o.rt.Log().Info("FR below threshold", slog.Float64("fr", td.FundingRate*100))
		o.abort("scan", "FR below threshold")
		return
	}

	candidate := o.rt.BuildCandidate(td)
	if !o.rt.Enrich(ctx, &candidate) {
		o.abort("scan", "enrichment failed")
		return
	}
	o.rt.SetCandidate(candidate)

	spread := cycle.CalcSpreadPct(candidate.BestBid, candidate.BestAsk)
	reqID := o.rt.GetReqID()
	o.rt.RecordAndPublish(reqID, events.TopicScanCandidateFound, events.CandidateFoundEvent{
		Flow:        events.FlowScan,
		Symbol:      candidate.Symbol,
		FundingRate: candidate.FundingRate,
		Side:        candidate.Side,
		CloseSide:   candidate.CloseSide,
		LastPrice:   candidate.LastPrice,
		BestBid:     candidate.BestBid,
		BestAsk:     candidate.BestAsk,
		Vol24h:      candidate.Volume24,
		SpreadPct:   spread,
	})

	o.rt.Log().Info("Qualified",
		slog.String("side", candidate.Side.String()),
		slog.Float64("fr", candidate.FundingRate*100),
	)

	scanEvent := events.CandidateFoundEvent{
		Flow:        events.FlowReversion,
		Symbol:      candidate.Symbol,
		FundingRate: candidate.FundingRate,
		Side:        candidate.Side,
		CloseSide:   candidate.CloseSide,
		LastPrice:   candidate.LastPrice,
		BestBid:     candidate.BestBid,
		BestAsk:     candidate.BestAsk,
		Vol24h:      candidate.Volume24,
		SpreadPct:   spread,
	}
	o.rt.RecordAndPublish(reqID, events.TopicReversionCandidate, scanEvent)

	if cfg.IsHedgeTrapEnabled() {
		trapEvent := scanEvent
		trapEvent.Flow = events.FlowTrap
		trapEvent.Side = candidate.Side.Opposite()
		trapEvent.CloseSide = shared.CloseSideFor(trapEvent.Side)
		o.rt.RecordAndPublish(reqID, events.TopicTrapCandidate, trapEvent)
	}
}
