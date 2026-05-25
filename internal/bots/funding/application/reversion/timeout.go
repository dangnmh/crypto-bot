package reversion

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/infrastructure/exchange"
	applogger "crypto-bot/pkg/logger"
)

func (s *Strategy) handleIOCFired(ctx context.Context, evt IOCFiredEvent) error {
	if evt.Error != "" {
		// If IOC failed, do nothing, cleanup handler will shut it down
		return nil
	}

	timeout := time.Duration(s.cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// 1. Subscribe watcher to personal position updates
	s.deps.OrderNotifier.OnPositionUpdate(ctx, evt.Symbol, timeout*2, func(pos exchange.PersonalPositionUpdate) {
		s.handlePositionUpdate(ctx, pos)
	})

	// 2. Start timeout guard in the background
	go func() {
		_ = s.timeoutGuard(ctx, evt)
	}()

	return nil
}

func (s *Strategy) timeoutGuard(ctx context.Context, firedEvt IOCFiredEvent) error {
	timeout := time.Duration(s.cfg.FundingReversion.PostSettleTimeout)
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	s.mu.Lock()
	settleTime := s.settleTime
	s.mu.Unlock()

	startedAt := time.Now()
	applogger.WithCtx(ctx, s.log).Info("Reversion timeout guard started",
		slog.Duration("timeout", timeout),
	)

	target := startedAt.Add(timeout)
	if !settleTime.IsZero() {
		target = settleTime.Add(timeout)
	}

	if !s.WaitUntil(ctx, target) {
		return ctx.Err()
	}

	// Check if already closed or terminal
	if s.isTerminal() {
		return nil
	}

	fill, filled := s.getFill()
	if filled && fill.FillVol > 0 {
		s.forceCloseTimedOutPosition(ctx, fill, timeout, startedAt)
	} else {
		// Timeout without any fill
		s.mu.Lock()
		sym := s.cfg.Symbol
		s.mu.Unlock()

		evt := TimeoutEvent{
			Flow:                FlowReversion,
			Symbol:              sym,
			Timeout:             timeout,
			Reason:              reversionReasonNoFill,
			ForceCloseAttempted: false,
			Timestamp:           s.deps.Clock.Now(),
		}
		_ = s.publishEvent(ctx, TopicReversionTimeout, evt)
		s.abort(ctx, reversionReasonNoFill)
	}

	return nil
}

func (s *Strategy) forceCloseTimedOutPosition(
	ctx context.Context,
	fill application.FillInfo,
	timeout time.Duration,
	startedAt time.Time,
) {
	closeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	s.mu.Lock()
	sym := s.order.Symbol
	s.mu.Unlock()

	retries, err := s.forceClosePosition(closeCtx, sym, 3)
	if err != nil {
		s.publishReversionCritical(closeCtx, sym, "critical_timeout_close_failed: "+err.Error())
		return
	}

	now := time.Now()
	timeoutEvt := TimeoutEvent{
		Flow:                FlowReversion,
		Symbol:              sym,
		Timeout:             timeout,
		Reason:              "force_close",
		ForceCloseAttempted: true,
		ForceCloseSucceeded: true,
		CloseRetryCount:     retries,
		Timestamp:           s.deps.Clock.Now(),
	}
	_ = s.publishEvent(ctx, TopicReversionTimeout, timeoutEvt)

	closeEvt := PositionClosedEvent{
		Flow:            FlowReversion,
		Symbol:          sym,
		CloseVol:        fill.FillVol,
		Reason:          "timeout_force_close",
		Method:          reversionMethodFallbackClose,
		HoldDurationMs:  now.Sub(startedAt).Milliseconds(),
		CloseRetryCount: retries,
		Timestamp:       s.deps.Clock.Now(),
	}
	_ = s.publishEvent(ctx, TopicReversionPositionClosed, closeEvt)
}
