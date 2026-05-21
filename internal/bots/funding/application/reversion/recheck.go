package reversion

import (
	"context"
	"log/slog"
	"math"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeRecheck(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionWaitComplete, func(_ *message.Message) {
		handleRecheck(ctx, rt)
	})
}

func handleRecheck(ctx context.Context, rt *cycle.Runtime) {
	c := rt.CandidateCopy()
	cfg := rt.Config()
	reqID := rt.GetReqID()
	td, err := rt.Deps().TickerStore.GetTicker(ctx, c.Symbol)
	if err != nil {
		rt.Log().Warn("No ticker for recheck")
		rt.Abort(reqID, "recheck", "no ticker")
		return
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		rt.Log().Error("FR sign flip!",
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		rt.RecordAndPublish(reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
			Flow:        events.FlowReversion,
			Symbol:      c.Symbol,
			FundingRate: td.FundingRate,
			FRChanged:   true,
		})
		rt.Abort(reqID, "recheck", "FR sign flip")
		return
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		rt.Log().Warn("FR dropped below threshold",
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		rt.RecordAndPublish(reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
			Flow:        events.FlowReversion,
			Symbol:      c.Symbol,
			FundingRate: td.FundingRate,
			FRChanged:   true,
		})
		rt.Abort(reqID, "recheck", "FR below threshold")
		return
	}

	rt.Log().Info("FR OK", slog.Float64("fr", td.FundingRate*100))
	rt.RecordAndPublish(reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
		Flow:        events.FlowReversion,
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Side:        c.Side,
		CloseSide:   c.CloseSide,
	})
}
