package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/events"
	fundingdomain "crypto-bot/internal/bots/funding/domain"
	applogger "crypto-bot/pkg/logger"

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
		applogger.WithCtx(ctx, rt.Log()).Error("Failed to subscribe WS channels", slog.Any("error", err))
		rt.AbortCtx(ctx, reqID, "arm", "WS subscribe failed")
		return
	}

	if err := waitForFreshPrice(ctx, rt, c.Symbol, 5*time.Second); err != nil {
		applogger.WithCtx(ctx, rt.Log()).Warn("Price data wait failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.AbortCtx(ctx, reqID, "arm", "refresh price failed")
		return
	}
	if err := rt.RefreshPrice(ctx, &c); err != nil {
		applogger.WithCtx(ctx, rt.Log()).Warn("Refresh price failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.AbortCtx(ctx, reqID, "arm", "refresh price failed")
		return
	}

	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		applogger.WithCtx(ctx, rt.Log()).Warn("IOC calc failed", slog.Any("error", err))
		rt.UnsubscribeAll(ctx)
		rt.AbortCtx(ctx, reqID, "arm", "IOC calc failed")
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

	safety := rt.Global().System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})
	if !c.SafetyResult.Passed {
		rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionArmed, events.ArmedEvent{
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
			Volume:             c.Volume,
			DesiredNotional:    c.SafetyResult.DesiredNotionalUSDT,
			ActualNotional:     c.SafetyResult.ActualNotionalUSDT,
			MaxSafeNotional:    c.SafetyResult.MaxSafeNotionalUSDT,
		})
		applogger.WithCtx(ctx, rt.Log()).Warn("Safety FAIL", slog.String("reason", c.SafetyResult.RejectReason))
		rt.UnsubscribeAll(ctx)
		rt.AbortCtx(ctx, reqID, "arm", c.SafetyResult.RejectReason)
		return
	}

	armed := events.ArmedEvent{
		Flow:            events.FlowReversion,
		Symbol:          c.Symbol,
		FundingRate:     c.FundingRate,
		Side:            c.Side,
		CloseSide:       c.CloseSide,
		LastPrice:       c.LastPrice,
		BestBid:         c.BestBid,
		BestAsk:         c.BestAsk,
		SafetyPassed:    true,
		Volume:          c.Volume,
		DesiredNotional: c.SafetyResult.DesiredNotionalUSDT,
		ActualNotional:  c.SafetyResult.ActualNotionalUSDT,
		MaxSafeNotional: c.SafetyResult.MaxSafeNotionalUSDT,
	}

	pd, err := rt.Deps().PriceStore.GetPrice(ctx, cfg.Symbol, 5*time.Second)
	if err == nil {
		armed.LastPrice = pd.LastPrice
		armed.BestBid = pd.BestBid
		armed.BestAsk = pd.BestAsk
	}

	rt.SetCandidate(c)
	rt.RecordAndPublishCtx(ctx, reqID, events.TopicReversionArmed, armed)
	applogger.WithCtx(ctx, rt.Log()).Info("Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", ioc),
		slog.Float64("vol", c.Volume),
		slog.Float64("desiredNotionalUSDT", c.SafetyResult.DesiredNotionalUSDT),
		slog.Float64("actualNotionalUSDT", c.SafetyResult.ActualNotionalUSDT),
		slog.Float64("maxSafeNotionalUSDT", c.SafetyResult.MaxSafeNotionalUSDT),
		slog.Float64("avgMinuteVolumeUSDT", c.SafetyResult.AvgMinuteVolumeUSDT),
		slog.Bool("sizedDown", c.SafetyResult.SizedDown),
	)
}

func waitForFreshPrice(ctx context.Context, rt *cycle.Runtime, symbol string, maxWait time.Duration) error {
	priceStore := rt.Deps().PriceStore
	if priceStore == nil {
		return fmt.Errorf("price store unavailable")
	}

	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	updates := priceStore.SubscribePrice(waitCtx, symbol)
	if _, err := priceStore.GetPrice(ctx, symbol, maxWait); err == nil {
		return nil
	}

	for {
		select {
		case <-waitCtx.Done():
			return fmt.Errorf("wait for price data %s: %w", symbol, waitCtx.Err())
		case pd, ok := <-updates:
			if !ok {
				return fmt.Errorf("wait for price data %s: %w", symbol, waitCtx.Err())
			}
			if pd != nil {
				return nil
			}
		}
	}
}
