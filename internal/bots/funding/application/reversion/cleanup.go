package reversion

import (
	"context"
)

func (s *Strategy) handleCleanup(ctx context.Context, msgUUID string, metadata map[string]string) error {
	s.mu.Lock()
	doneChan := s.doneChan
	hasFill := s.initialPosition != nil
	s.mu.Unlock()

	defer func() {
		// Recover if doneChan is already closed
		defer func() { _ = recover() }()
		close(doneChan)
	}()

	s.unsubscribeWS(ctx)

	if hasFill {
		// Calculate final PnL from Strategy state
		finalEvt := s.calculateFinalPnL()
		_ = s.publishEvent(ctx, TopicReversionFinalPnL, finalEvt)
	}

	compEvt := ReversionCompletedEvent{
		Flow:       FlowReversion,
		Symbol:     s.cfg.Symbol,
		Reason:     "cleanup_finished",
		SendNotify: false,
		Timestamp:  s.deps.Clock.Now(),
	}
	_ = s.publishEvent(ctx, TopicReversionCompleted, compEvt)

	s.tryMarkTerminal()
	return nil
}

func (s *Strategy) calculateFinalPnL() FinalPnLEvent {
	s.mu.Lock()
	symbol := s.cfg.Symbol
	initPos := s.initialPosition
	latePos := s.latestPosition
	s.mu.Unlock()

	evt := FinalPnLEvent{
		Flow:       FlowReversion,
		Symbol:     symbol,
		Timestamp:  s.deps.Clock.Now(),
		SendNotify: true,
	}

	if initPos == nil || latePos == nil {
		return evt
	}

	// 1. Direction
	evt.Direction = s.candidate.Side

	// 2. Entry Price
	entryPrice := initPos.OpenAvgPrice
	if entryPrice == 0 {
		entryPrice = initPos.HoldAvgPrice
	}
	if entryPrice == 0 {
		entryPrice = s.candidate.LastPrice
	}
	evt.EntryPrice = entryPrice

	// 3. Close Price
	closePrice := latePos.CloseAvgPrice
	if closePrice == 0 {
		closePrice = latePos.HoldAvgPrice
	}
	if closePrice == 0 {
		closePrice = s.candidate.LastPrice
	}
	evt.ClosePrice = closePrice

	// 4. Max Vol
	evt.MaxVol = initPos.HoldVol

	// 5. Gross PnL
	evt.GrossPnL = latePos.CloseProfitLoss

	// 6. Fees
	evt.Fees = latePos.Fee
	evt.HoldFee = latePos.HoldFee

	// 7. Net PnL
	evt.NetPnL = latePos.CloseProfitLoss - latePos.Fee

	// 8. Hold Duration
	if latePos.UpdateTime > initPos.UpdateTime {
		evt.HoldDurationMs = latePos.UpdateTime - initPos.UpdateTime
	}

	return evt
}

func (s *Strategy) publishReversionCritical(ctx context.Context, symbol, reason string) {
	if s.tryMarkTerminal() {
		errEvt := ErrorEvent{
			Flow:       FlowReversion,
			Symbol:     symbol,
			Error:      reason,
			Timestamp:  s.deps.Clock.Now(),
			SendNotify: true,
		}
		_ = s.publishEvent(ctx, TopicReversionError, errEvt)

		abortEvt := AbortEvent{
			Flow:       FlowReversion,
			Symbol:     symbol,
			Reason:     reason,
			Timestamp:  s.deps.Clock.Now(),
			SendNotify: false,
		}
		_ = s.publishEvent(ctx, TopicReversionAbort, abortEvt)
	}
}
