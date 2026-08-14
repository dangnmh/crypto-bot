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
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
)

func (r *StatelessRunner) handleArm(ctx context.Context, startEvt CandidateFoundEvent) error {
	r.log.InfoContext(ctx, "handleArm SettleTime", slog.Time("settle", startEvt.SettleTime))
	type syncer interface {
		SyncNow(ctx context.Context)
	}
	if s, ok := r.deps.Clock.(syncer); ok {
		r.log.InfoContext(ctx, "Forcing clock sync on arm")
		s.SyncNow(ctx)
	}
	c := startEvt.Candidate
	maxWait := 5 * time.Second

	if err := r.subscribeWS(ctx, c.Symbol); err != nil {
		r.log.ErrorContext(ctx, "Failed to subscribe WS channels", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, ReversionReason("WS subscribe failed: "+err.Error()))
		return fmt.Errorf("WS subscribe failed: %w", err)
	}

	if err := r.waitForFreshPrice(ctx, c.Symbol, maxWait); err != nil {
		r.log.WarnContext(ctx, "Price data wait failed", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, ReversionReason("fresh price wait failed: "+err.Error()))
		return fmt.Errorf("refresh price failed: %w", err)
	}

	if err := r.refreshPrice(ctx, &c); err != nil {
		r.log.WarnContext(ctx, "Refresh price failed", slog.Any("error", err))
		r.abortAfter(ctx, startEvt.BaseReversionEvent, c.Symbol, ReversionReason("refresh price failed: "+err.Error()))
		return fmt.Errorf("refresh price failed: %w", err)
	}

	evt := ArmMarketReadyEvent{
		BaseReversionEvent: nextReversionBase(startEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		MaxWaitMs:          maxWait.Milliseconds(),
	}

	return r.publishEvent(ctx, TopicReversionArmMarketReady, evt)
}

func (r *StatelessRunner) handleArmMarketReady(ctx context.Context, evt ArmMarketReadyEvent) error {
	c := evt.Candidate
	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		r.log.WarnContext(ctx, "IOC calc failed", slog.Any("error", err))
		r.abortAfter(ctx, evt.BaseReversionEvent, c.Symbol, ReversionReason("IOC calc failed: "+err.Error()))
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
		IOCPrice:           ioc,
		RefPrice:           refPrice,
	}

	return r.publishEvent(ctx, TopicReversionArmPlanCalculated, next)
}

func (r *StatelessRunner) handleArmPlanCalculated(ctx context.Context, evt ArmPlanCalculatedEvent) error {
	c := evt.Candidate
	safety := r.globalCfg.Reversion.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    c.Config.MinVol24USD,
	})

	rejectReason := ""
	passed := c.SafetyResult != nil && c.SafetyResult.Passed
	if c.SafetyResult != nil {
		rejectReason = c.SafetyResult.RejectReason
	}

	next := SafetyCheckedEvent{
		BaseReversionEvent: nextReversionBase(evt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
		IOCPrice:           evt.IOCPrice,
		RefPrice:           evt.RefPrice,
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
		r.abortAfter(ctx, safetyEvt.BaseReversionEvent, c.Symbol, ReversionReason("safety fail: "+safetyEvt.RejectReason))
		return fmt.Errorf("safety fail: %s", safetyEvt.RejectReason)
	}

	r.log.InfoContext(ctx, "Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", safetyEvt.IOCPrice),
		slog.Float64("vol", c.Volume),
	)

	evt := ArmedEvent{
		BaseReversionEvent: nextReversionBase(safetyEvt.BaseReversionEvent, c.Symbol, r.deps.Clock.Now()),
		Candidate:          c,
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
		if r.deps.Client != nil {
			return r.fallbackRESTPrice(ctx, symbol)
		}
		return nil
	}

	waitCtx, cancel := context.WithTimeout(ctx, maxWait)
	defer cancel()

	updates := priceStore.SubscribePrice(waitCtx, symbol)
	if pd, err := priceStore.GetPrice(ctx, symbol, maxWait); err == nil && pd.BestBid > 0 && pd.BestAsk > 0 {
		return nil
	}

	for {
		select {
		case <-waitCtx.Done():
			r.log.WarnContext(ctx, "Price data wait timed out, falling back to REST API ticker query", slog.String("symbol", symbol))
			if err := r.fallbackRESTPrice(ctx, symbol); err != nil {
				return fmt.Errorf("wait for price data %s: timed out and REST fallback failed: %w", symbol, err)
			}
			return nil
		case pd, ok := <-updates:
			if !ok {
				r.log.WarnContext(ctx, "Price subscription closed, falling back to REST API ticker query", slog.String("symbol", symbol))
				if err := r.fallbackRESTPrice(ctx, symbol); err != nil {
					return fmt.Errorf("wait for price data %s: sub closed and REST fallback failed: %w", symbol, err)
				}
				return nil
			}
			if pd != nil && pd.BestBid > 0 && pd.BestAsk > 0 {
				return nil
			}
		}
	}
}

func (r *StatelessRunner) fallbackRESTPrice(ctx context.Context, symbol string) error {
	tickers, err := r.deps.Client.GetTickers(ctx, symbol)
	if err != nil {
		return err
	}
	var found *exchange.Ticker
	for _, t := range tickers {
		if t.Symbol == symbol {
			found = &t
			break
		}
	}
	if found == nil {
		return fmt.Errorf("symbol not found in REST tickers")
	}
	pd := &store.PriceData{
		Symbol:    found.Symbol,
		LastPrice: found.LastPrice,
		BestBid:   found.Bid1,
		BestAsk:   found.Ask1,
		UpdatedAt: time.Now(),
	}
	writer, ok := r.deps.PriceStore.(store.PriceWriter)
	if !ok {
		return fmt.Errorf("price store does not support PriceWriter")
	}
	writer.UpdatePrice(symbol, pd)
	r.log.WarnContext(ctx, "Successfully fetched and updated price from REST fallback", slog.String("symbol", symbol), slog.Float64("price", pd.LastPrice))
	return nil
}
