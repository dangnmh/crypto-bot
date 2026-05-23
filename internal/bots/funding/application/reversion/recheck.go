package reversion

import (
	"context"
	"log/slog"
	"math"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	applogger "crypto-bot/pkg/logger"

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
		applogger.WithCtx(ctx, rt.Log()).Warn("No ticker for recheck")
		rt.AbortCtx(ctx, reqID, "recheck", "no ticker")
		return
	}

	if (td.FundingRate > 0) != (c.FundingRate > 0) {
		applogger.WithCtx(ctx, rt.Log()).Error("FR sign flip!",
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", td.FundingRate*100),
		)
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
			Flow:        events.FlowReversion,
			Symbol:      c.Symbol,
			FundingRate: td.FundingRate,
			FRChanged:   true,
		})
		rt.AbortCtx(ctx, reqID, "recheck", "FR sign flip")
		return
	}

	if math.Abs(td.FundingRate) < cfg.MinFundingRate {
		applogger.WithCtx(ctx, rt.Log()).Warn("FR dropped below threshold",
			slog.Float64("fr", td.FundingRate*100),
			slog.Float64("min", cfg.MinFundingRate*100),
		)
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
			Flow:        events.FlowReversion,
			Symbol:      c.Symbol,
			FundingRate: td.FundingRate,
			FRChanged:   true,
		})
		rt.AbortCtx(ctx, reqID, "recheck", "FR below threshold")
		return
	}

	applogger.WithCtx(ctx, rt.Log()).Info("FR OK", slog.Float64("fr", td.FundingRate*100))
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionConfirmed, events.ConfirmedEvent{
		Flow:        events.FlowReversion,
		Symbol:      c.Symbol,
		FundingRate: td.FundingRate,
		Side:        c.Side,
		CloseSide:   c.CloseSide,
	})
}
