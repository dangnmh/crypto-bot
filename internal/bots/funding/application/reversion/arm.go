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
	applogger "crypto-bot/pkg/logger"
)

func (s *Strategy) handleArm(ctx context.Context, startEvt CandidateFoundEvent) error {
	c := startEvt.Candidate

	if err := s.subscribeWS(ctx); err != nil {
		applogger.WithCtx(ctx, s.log).Error("Failed to subscribe WS channels", slog.Any("error", err))
		s.abort(ctx, "WS subscribe failed: "+err.Error())
		return fmt.Errorf("WS subscribe failed: %w", err)
	}

	if err := s.waitForFreshPrice(ctx, c.Symbol, 5*time.Second); err != nil {
		applogger.WithCtx(ctx, s.log).Warn("Price data wait failed", slog.Any("error", err))
		s.unsubscribeWS(ctx)
		s.abort(ctx, "refresh price failed: "+err.Error())
		return fmt.Errorf("refresh price failed: %w", err)
	}

	if err := s.refreshPrice(ctx, &c); err != nil {
		applogger.WithCtx(ctx, s.log).Warn("Refresh price failed", slog.Any("error", err))
		s.unsubscribeWS(ctx)
		s.abort(ctx, "refresh price failed: "+err.Error())
		return fmt.Errorf("refresh price failed: %w", err)
	}

	ioc, err := c.CalculateIOCPrice()
	if err != nil {
		applogger.WithCtx(ctx, s.log).Warn("IOC calc failed", slog.Any("error", err))
		s.unsubscribeWS(ctx)
		s.abort(ctx, "IOC calc failed: "+err.Error())
		return fmt.Errorf("IOC calc failed: %w", err)
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

	safety := s.global.System.Safety
	c.SafetyResult = c.ApplySafetySizing(fundingdomain.SafetyLimits{
		MaxImpactRatio: safety.MaxImpactRatio,
		MinVol24USD:    safety.MinVol24USD,
	})
	if !c.SafetyResult.Passed {
		applogger.WithCtx(ctx, s.log).Warn("Safety FAIL", slog.String("reason", c.SafetyResult.RejectReason))
		s.unsubscribeWS(ctx)
		s.abort(ctx, "safety fail: "+c.SafetyResult.RejectReason)
		return fmt.Errorf("safety fail: %s", c.SafetyResult.RejectReason)
	}

	s.setCandidate(c)
	applogger.WithCtx(ctx, s.log).Info("Ready",
		slog.String("side", c.Side.String()),
		slog.Float64("fr", c.FundingRate*100),
		slog.Float64("ioc", ioc),
		slog.Float64("vol", c.Volume),
	)

	evt := ArmedEvent{
		Flow:       FlowReversion,
		Symbol:     c.Symbol,
		Candidate:  c,
		Volume:     c.Volume,
		IOCPrice:   ioc,
		Slippage:   c.Slippage,
		Timestamp:  s.deps.Clock.Now(),
		SendNotify: true,
	}

	return s.publishEvent(ctx, TopicReversionArmed, evt)
}

func (s *Strategy) waitForFreshPrice(ctx context.Context, symbol string, maxWait time.Duration) error {
	priceStore := s.deps.PriceStore
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
