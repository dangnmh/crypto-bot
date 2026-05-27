package reversion

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"

	fundingdomain "crypto-bot/internal/bots/funding/domain"
)

func (r *StatelessRunner) handleArm(ctx context.Context, startEvt CandidateFoundEvent) error {
	r.log.InfoContext(ctx, "handleArm SettleTime", slog.Time("settle", startEvt.SettleTime))
	c := startEvt.Candidate
	maxWait := 5 * time.Second

	if err := r.subscribeWS(ctx, c.Symbol); err != nil {
		r.log.ErrorContext(ctx, "Failed to subscribe WS channels", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, "WS subscribe failed: "+err.Error())
		return fmt.Errorf("WS subscribe failed: %w", err)
	}

	if err := r.waitForFreshPrice(ctx, c.Symbol, maxWait); err != nil {
		r.log.WarnContext(ctx, "Price data wait failed", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, "fresh price wait failed: "+err.Error())
		return fmt.Errorf("refresh price failed: %w", err)
	}

	if err := r.refreshPrice(ctx, &c); err != nil {
		r.log.WarnContext(ctx, "Refresh price failed", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, "refresh price failed: "+err.Error())
		return fmt.Errorf("refresh price failed: %w", err)
	}

	evt := ArmMarketReadyEvent{
		BaseReversionEvent: nextReversionBase(startEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		SettleTime:         startEvt.SettleTime,
		MaxWaitMs:          maxWait.Milliseconds(),
		BestBid:            c.BestBid,
		BestAsk:            c.BestAsk,
		LastPrice:          c.LastPrice,
	}

	return r.publishEvent(ctx, TopicReversionArmMarketReady, evt)
}

func (r *StatelessRunner) handleArmMarketReady(ctx context.Context, evt ArmMarketReadyEvent) error {
	c := evt.Candidate
	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		r.log.WarnContext(ctx, "IOC calc failed", slog.Any("error", err))
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, "IOC calc failed: "+err.Error())
		return fmt.Errorf("IOC calc failed: %w", err)
	}

	refPrice := executionRefPrice(c)
	if ioc > 0 && refPrice > 0 {
		c.Slippage = decmath.Mul(decmath.Div(math.Abs(decmath.Sub(ioc, refPrice)), refPrice), 100.0)
	}
	c.Volume = c.CalculateVolume()

	next := ArmPlanCalculatedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		SettleTime:         evt.SettleTime,
		IOCPrice:           ioc,
		RefPrice:           refPrice,
		Slippage:           c.Slippage,
		RequestedVolume:    c.Volume,
	}

	return r.publishEvent(ctx, TopicReversionArmPlanCalculated, next)
}

func (r *StatelessRunner) handleArmPlanCalculated(ctx context.Context, evt ArmPlanCalculatedEvent) error {
	c := evt.Candidate
	safety := r.globalCfg.System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})

	rejectReason := ""
	passed := c.SafetyResult != nil && c.SafetyResult.Passed
	if c.SafetyResult != nil {
		rejectReason = c.SafetyResult.RejectReason
	}

	next := SafetyCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		SettleTime:         evt.SettleTime,
		IOCPrice:           evt.IOCPrice,
		RefPrice:           evt.RefPrice,
		Slippage:           evt.Slippage,
		RequestedVolume:    evt.RequestedVolume,
		AdjustedVolume:     c.Volume,
		Passed:             passed,
		RejectReason:       rejectReason,
	}

	return r.publishEvent(ctx, TopicReversionSafetyChecked, next)
}

func (r *StatelessRunner) handleSafetyChecked(ctx context.Context, safetyEvt SafetyCheckedEvent) error {
	c := safetyEvt.Candidate
	if !safetyEvt.Passed {
		r.log.WarnContext(ctx, "Safety FAIL", slog.String("reason", safetyEvt.RejectReason))
		r.abortAfter(ctx, safetyEvt.BaseReversionEvent, c.Symbol, "safety fail: "+safetyEvt.RejectReason)
		return fmt.Errorf("safety fail: %s", safetyEvt.RejectReason)
	}

	r.log.InfoContext(ctx, "Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", safetyEvt.IOCPrice),
		slog.Float64("vol", c.Volume),
	)

	evt := ArmedEvent{
		BaseReversionEvent: nextNotifyReversionBase(safetyEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		Volume:             c.Volume,
		IOCPrice:           safetyEvt.IOCPrice,
		Slippage:           c.Slippage,
		SettleTime:         safetyEvt.SettleTime,
	}

	return r.publishEvent(ctx, TopicReversionArmed, evt)
}

func executionRefPrice(c fundingdomain.Candidate) float64 {
	if c.Side == shared.SideOpenLong {
		return c.BestAsk
	}
	return c.BestBid
}

func (r *StatelessRunner) waitForFreshPrice(ctx context.Context, symbol string, maxWait time.Duration) error {
	priceStore := r.deps.PriceStore
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
