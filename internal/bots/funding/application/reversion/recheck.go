package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	applogger "crypto-bot/pkg/logger"
)

func (r *StatelessRunner) handleRecheck(ctx context.Context, waitEvt WaitCompleteEvent) error {
	r.log.Info("handleRecheck SettleTime", slog.Time("settle", waitEvt.SettleTime))
	c := waitEvt.Candidate
	cfg, ok := r.getSymbolConfig(waitEvt.Symbol)
	if !ok {
		r.log.Error("Symbol config not found for recheck", slog.String("symbol", waitEvt.Symbol))
		r.abort(ctx, waitEvt.Symbol, "symbol config not found")
		return fmt.Errorf("symbol config not found")
	}

	td, err := r.deps.TickerStore.GetTicker(ctx, c.Symbol)
	if err != nil {
		applogger.WithCtx(ctx, r.log).Warn("No ticker for recheck", slog.String("symbol", c.Symbol))
		r.abort(ctx, c.Symbol, "no ticker for recheck")
		return fmt.Errorf("no ticker for recheck")
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		applogger.WithCtx(ctx, r.log).Error("FR sign flip!",
			slog.String("symbol", c.Symbol),
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		r.abort(ctx, c.Symbol, "FR sign flip")
		return fmt.Errorf("FR sign flip")
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		applogger.WithCtx(ctx, r.log).Warn("FR dropped below threshold",
			slog.String("symbol", c.Symbol),
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		r.abort(ctx, c.Symbol, "FR below threshold")
		return fmt.Errorf("FR below threshold")
	}

	applogger.WithCtx(ctx, r.log).Info("FR OK", slog.String("symbol", c.Symbol), slog.Float64("fr", td.FundingRate*100))

	evt := ConfirmedEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			Symbol:    c.Symbol,
			Timestamp: r.deps.Clock.Now(),
		},
		FundingRate: td.FundingRate,
		Candidate:   c,
		SettleTime:  waitEvt.SettleTime,
	}

	return r.publishEvent(ctx, TopicReversionConfirmed, evt)
}
