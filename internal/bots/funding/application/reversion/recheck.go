package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"
)

func (r *StatelessRunner) handleRecheck(ctx context.Context, waitEvt WaitCompleteEvent) error {
	r.log.InfoContext(ctx, "handleRecheck SettleTime", slog.Time("settle", waitEvt.SettleTime))
	type syncer interface {
		SyncNow(ctx context.Context)
	}
	if s, ok := r.deps.Clock.(syncer); ok {
		r.log.InfoContext(ctx, "Forcing clock sync before recheck")
		s.SyncNow(ctx)
	}
	c := waitEvt.Candidate

	rates, err := r.deps.Client.GetFundingRates(ctx, []string{c.Symbol})
	if err != nil {
		r.log.WarnContext(ctx, "Failed to fetch funding rate for recheck", slog.String("symbol", c.Symbol), slog.Any("error", err))
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, ReversionReason("no funding data for recheck"))
		return fmt.Errorf("no funding data for recheck: %w", err)
	}
	if len(rates) == 0 {
		r.log.WarnContext(ctx, "No funding rates returned for recheck", slog.String("symbol", c.Symbol))
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, ReversionReason("no funding data for recheck"))
		return fmt.Errorf("no funding data for recheck")
	}

	fundingRate := rates[0].Rate

	if (fundingRate > 0) != (c.FundingRate > 0) {
		r.log.ErrorContext(ctx, "FR sign flip!",
			slog.String("symbol", c.Symbol),
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", fundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, ReversionReason("FR sign flip"))
		return fmt.Errorf("FR sign flip")
	}

	if math.Abs(fundingRate) < c.Config.MinFundingRate {
		r.log.WarnContext(ctx, "FR dropped below threshold",
			slog.String("symbol", c.Symbol),
			slog.Float64("fr", fundingRate*100),
			slog.Float64("min", c.Config.MinFundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, ReversionReason("FR below threshold"))
		return fmt.Errorf("FR below threshold")
	}

	r.log.InfoContext(ctx, "FR OK", slog.String("symbol", c.Symbol), slog.Float64("fr", fundingRate*100))

	base := nextReversionBase(waitEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now())
	base.FundingRate = fundingRate
	evt := ConfirmedEvent{
		BaseReversionEvent: base,
		Candidate:          c,
	}

	return r.publishEvent(ctx, TopicReversionConfirmed, evt)
}
