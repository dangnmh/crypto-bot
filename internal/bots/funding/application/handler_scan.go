package application

import (
	"context"
	"log/slog"
	"math"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/domain"

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
		o.rt.Log().Warn("🟡 No ticker", slog.Any("error", err))
		o.abort("scan", "no ticker data")
		return
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		o.rt.Log().Info("😴 FR below threshold", slog.Float64("fr", td.FundingRate*100))
		o.abort("scan", "FR below threshold")
		return
	}

	candidate := o.rt.BuildCandidate(td)
	if !o.rt.Enrich(ctx, &candidate) {
		o.abort("scan", "enrichment failed")
		return
	}
	o.rt.SetCandidate(candidate)

	o.rt.MutateRecorder(func(b *domain.CycleRecordBuilder) {
		b.FRAtScan = td.FundingRate
		b.Side = candidate.Side
	})
	spread := cycle.CalcSpreadPct(candidate.BestBid, candidate.BestAsk)
	o.rt.Recorder().AddSnapshot(domain.MarketSnapshot{
		Topic:     events.TopicScanCandidateFound,
		LastPrice: candidate.LastPrice,
		BestBid:   candidate.BestBid,
		BestAsk:   candidate.BestAsk,
		Spread:    spread,
	})

	o.rt.Log().Info("🔍 Qualified",
		slog.String("side", candidate.Side.String()),
		slog.Float64("fr", candidate.FundingRate*100),
	)

	scanEvent := events.CandidateFoundEvent{
		Symbol:      candidate.Symbol,
		FundingRate: candidate.FundingRate,
		Side:        candidate.Side,
		CloseSide:   candidate.CloseSide,
		LastPrice:   candidate.LastPrice,
	}
	o.publishOrLog(events.TopicScanCandidateFound, scanEvent)

	if cfg.FundingReversion.Enabled {
		reversionEvent := scanEvent
		reversionEvent.Flow = events.FlowReversion
		o.publishOrLog(events.TopicReversionCandidate, reversionEvent)
	}

	if cfg.IsHedgeTrapEnabled() {
		trapEvent := scanEvent
		trapEvent.Flow = events.FlowTrap
		trapEvent.Side = candidate.Side.Opposite()
		trapEvent.CloseSide = shared.CloseSideFor(trapEvent.Side)
		o.publishOrLog(events.TopicTrapCandidate, trapEvent)
	}
}
