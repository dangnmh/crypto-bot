package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	applogger "crypto-bot/pkg/logger"
)

func (s *Strategy) handleRecheck(ctx context.Context, waitEvt WaitCompleteEvent) error {
	c := s.getCandidateCopy()
	cfg := s.cfg
	td, err := s.deps.TickerStore.GetTicker(ctx, c.Symbol)
	if err != nil {
		applogger.WithCtx(ctx, s.log).Warn("No ticker for recheck")
		s.abort(ctx, "no ticker for recheck")
		return fmt.Errorf("no ticker for recheck")
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		applogger.WithCtx(ctx, s.log).Error("FR sign flip!",
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		s.abort(ctx, "FR sign flip")
		return fmt.Errorf("FR sign flip")
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		applogger.WithCtx(ctx, s.log).Warn("FR dropped below threshold",
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		s.abort(ctx, "FR below threshold")
		return fmt.Errorf("FR below threshold")
	}

	applogger.WithCtx(ctx, s.log).Info("FR OK", slog.Float64("fr", td.FundingRate*100))

	evt := ConfirmedEvent{
		Flow:        FlowReversion,
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Timestamp:   s.deps.Clock.Now(),
	}

	return s.publishEvent(ctx, TopicReversionConfirmed, evt)
}
