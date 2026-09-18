package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
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
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "no funding data for recheck")
		return fmt.Errorf("no funding data for recheck: %w", err)
	}
	if len(rates) == 0 {
		r.log.WarnContext(ctx, "No funding rates returned for recheck", slog.String("symbol", c.Symbol))
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "no funding data for recheck")
		return fmt.Errorf("no funding data for recheck")
	}

	fundingRate := rates[0].Rate

	if (fundingRate > 0) != (c.FundingRate > 0) {
		r.log.ErrorContext(ctx, "FR sign flip!",
			slog.String("symbol", c.Symbol),
			slog.Float64("old", c.FundingRate*100),
			slog.Float64("new", fundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "FR sign flip")
		return ErrFRSignFlip
	}

	if math.Abs(fundingRate) < c.Config.MinFundingRate {
		r.log.WarnContext(ctx, "FR dropped below threshold",
			slog.String("symbol", c.Symbol),
			slog.Float64("fr", fundingRate*100),
			slog.Float64("min", c.Config.MinFundingRate*100),
		)
		r.abortAfter(ctx, waitEvt.BaseReversionEvent, c.Symbol, "FR below threshold")
		return ErrFRBelowThreshold
	}

	r.log.InfoContext(ctx, "FR OK", slog.String("symbol", c.Symbol), slog.Float64("fr", fundingRate*100))

	r.checkDepthImbalance(ctx, &c)

	base := nextReversionBase(waitEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now())
	base.FundingRate = fundingRate
	evt := ConfirmedEvent{
		BaseReversionEvent: base,
		Candidate:          c,
	}

	return r.publishEvent(ctx, TopicReversionConfirmed, evt)
}

func (r *StatelessRunner) checkDepthImbalance(ctx context.Context, c *fundingdomain.Candidate) {
	if c == nil {
		return
	}
	ob, err := r.fetchOrderBook(ctx, c.Symbol)
	if err != nil {
		r.log.WarnContext(ctx, "Failed to fetch orderbook for depth imbalance check",
			slog.String("symbol", c.Symbol),
			slog.Any("error", err),
		)
		return
	}

	// Near-market depth evaluation (within 1% of BBO, 1.5x threshold)
	res := fundingdomain.EvaluateDepthImbalance(ob, c.Side, 0.01, 1.5)
	c.ImbalanceRatio = res.Ratio

	if res.SuggestSkip {
		r.log.WarnContext(ctx, "⚠️ [DEPTH_IMBALANCE] High risk detected - suggest SKIP (logging only, entering order)",
			slog.String("symbol", c.Symbol),
			slog.String("side", c.Side.String()),
			slog.Float64("ratio", res.Ratio),
			slog.Float64("bid_notional", res.BidNotional),
			slog.Float64("ask_notional", res.AskNotional),
			slog.String("reason", res.SkipReason),
		)
	} else {
		r.log.InfoContext(ctx, "🟢 [DEPTH_IMBALANCE] Depth balance favorable",
			slog.String("symbol", c.Symbol),
			slog.String("side", c.Side.String()),
			slog.Float64("ratio", res.Ratio),
			slog.Float64("bid_notional", res.BidNotional),
			slog.Float64("ask_notional", res.AskNotional),
		)
	}
}

func (r *StatelessRunner) fetchOrderBook(ctx context.Context, symbol string) (*shared.OrderBook, error) {
	if r.deps.DepthStore != nil {
		if ob, err := r.deps.DepthStore.GetDepth(ctx, symbol); err == nil && ob != nil && len(ob.Bids) > 0 && len(ob.Asks) > 0 {
			return ob, nil
		}
	}
	if dp, ok := r.deps.Client.(exchange.DepthProvider); ok {
		return dp.GetDepth(ctx, symbol)
	}
	return nil, fmt.Errorf("no depth provider or depth store available for %s", symbol)
}
