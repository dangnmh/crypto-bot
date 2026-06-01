package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"
)

func (r *StatelessRunner) handleRecheck(ctx context.Context, waitEvt WaitCompleteEvent) error {
	r.log.InfoContext(ctx, "handleRecheck SettleTime", slog.Time("settle", waitEvt.SettleTime))
	c := waitEvt.Candidate
	cfg, ok := r.getSymbolConfig(waitEvt.Symbol)
	if !ok {
		r.log.ErrorContext(ctx, "Symbol config not found for recheck", slog.String("symbol", waitEvt.Symbol))
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, waitEvt.Symbol, "symbol config not found")
		return fmt.Errorf("symbol config not found")
	}

	fd, err := r.deps.FundingStore.GetFunding(ctx, c.Symbol)
	if err != nil {
		r.log.WarnContext(ctx, "No funding data for recheck", slog.String("symbol", c.Symbol))
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "no funding data for recheck")
		return fmt.Errorf("no funding data for recheck")
	}

	if (fd.FundingRate > 0) != (c.FundingRate > 0) {
		r.log.ErrorContext(ctx, "FR sign flip!",
			slog.String("symbol", c.Symbol),
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", fd.FundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "FR sign flip")
		return fmt.Errorf("FR sign flip")
	}

	if math.Abs(fd.FundingRate) < cfg.MinFundingRate {
		r.log.WarnContext(ctx, "FR dropped below threshold",
			slog.String("symbol", c.Symbol),
			slog.Float64("fr", fd.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "FR below threshold")
		return fmt.Errorf("FR below threshold")
	}

	r.log.InfoContext(ctx, "FR OK", slog.String("symbol", c.Symbol), slog.Float64("fr", fd.FundingRate*100))

	evt := ConfirmedEvent{
		BaseReversionEvent: nextReversionBase(waitEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		FundingRate:        fd.FundingRate,
		Candidate:          c,
	}

	return r.publishEvent(ctx, TopicReversionConfirmed, evt)
}
