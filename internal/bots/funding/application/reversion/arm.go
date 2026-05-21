package reversion

import (
	"context"
	"log/slog"
	"math"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"

	"github.com/ThreeDotsLabs/watermill/message"
)

func subscribeArm(ctx context.Context, rt *cycle.Runtime) {
	rt.Subscribe(ctx, events.TopicReversionCandidate, func(_ *message.Message) {
		handleArm(ctx, rt)
	})
}

func handleArm(ctx context.Context, rt *cycle.Runtime) {
	cfg := rt.Config()
	c := rt.CandidateCopy()
	reqID := rt.GetReqID()

	if err := rt.SubscribeAll(ctx); err != nil {
		rt.Log().Error("Failed to subscribe WS channels", slog.Any("error", err))
		rt.Abort(reqID, "arm", "WS subscribe failed")
		return
	}

	rt.Sleep(ctx, 2*time.Second)
	if err := rt.RefreshPrice(ctx, &c); err != nil {
		rt.Log().Warn("Refresh price failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.Abort(reqID, "arm", "refresh price failed")
		return
	}

	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		rt.Log().Warn("IOC calc failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.Abort(reqID, "arm", "IOC calc failed")
		return
	}
	c.Volume = c.CalculateVolume()

	if ioc > 0 {
		var refPrice float64
		if c.Side == shared.SideOpenLong {
			refPrice = c.BestAsk
		} else {
			refPrice = c.BestBid
		}
		if refPrice > 0 {
			c.Slippage = decmath.Mul(decmath.Div(math.Abs(decmath.Sub(ioc, refPrice)), refPrice), 100.0)
		}
	}

	c.SafetyResult = c.EvaluateSafety(rt.Global().System.Safety.MaxImpactRatio)
	if !c.SafetyResult.Passed {
		rt.RecordAndPublish(reqID, events.TopicReversionArmed, events.ArmedEvent{
			Flow:               events.FlowReversion,
			Symbol:             c.Symbol,
			FundingRate:        c.FundingRate,
			Side:               c.Side,
			CloseSide:          c.CloseSide,
			LastPrice:          c.LastPrice,
			BestBid:            c.BestBid,
			BestAsk:            c.BestAsk,
			SafetyPassed:       false,
			SafetyRejectReason: c.SafetyResult.RejectReason,
		})
		rt.Log().Warn("Safety FAIL", slog.String("reason", c.SafetyResult.RejectReason))
		rt.UnsubscribeAll(ctx)
		rt.Abort(reqID, "arm", c.SafetyResult.RejectReason)
		return
	}

	armed := events.ArmedEvent{
		Flow:         events.FlowReversion,
		Symbol:       c.Symbol,
		FundingRate:  c.FundingRate,
		Side:         c.Side,
		CloseSide:    c.CloseSide,
		LastPrice:    c.LastPrice,
		BestBid:      c.BestBid,
		BestAsk:      c.BestAsk,
		SafetyPassed: true,
	}

	pd, err := rt.Deps().PriceStore.GetPrice(ctx, cfg.Symbol, 5*time.Second)
	if err == nil {
		armed.LastPrice = pd.LastPrice
		armed.BestBid = pd.BestBid
		armed.BestAsk = pd.BestAsk
	}

	rt.SetCandidate(c)
	rt.RecordAndPublish(reqID, events.TopicReversionArmed, armed)
	rt.Log().Info("Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", ioc),
		slog.Float64("vol", c.Volume),
	)
}
