package reversion

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

func (s *Strategy) executeInternal(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error {
	ctx = observability.WithReversionID(ctx)

	s.mu.Lock()
	s.settleTime = settleTime
	s.candidate = candidate
	s.bus = eventbus.New(s.log)
	s.doneChan = make(chan struct{})
	s.terminal = false
	s.order = nil
	s.fill = nil
	s.mu.Unlock()

	defer func() {
		s.mu.Lock()
		if s.bus != nil {
			_ = s.bus.Close()
		}
		s.mu.Unlock()
	}()

	applogger.WithCtx(ctx, s.log).Info("🚀 Starting event-driven reversion bot lifecycle execution", slog.String("symbol", candidate.Symbol))

	// Register subscribers
	s.subscribeTopic(ctx, TopicReversionCandidate, s.handleArmMessage)
	s.subscribeTopic(ctx, TopicReversionArmed, s.handleWaitMessage)
	s.subscribeTopic(ctx, TopicReversionWaitComplete, s.handleRecheckMessage)
	s.subscribeTopic(ctx, TopicReversionConfirmed, s.handleFireIOCMessage)
	s.subscribeTopic(ctx, TopicReversionIOCFired, s.handleIOCFiredMessage)
	s.subscribeTopic(ctx, TopicReversionPositionClosed, s.handleCleanupMessage)
	s.subscribeTopic(ctx, TopicReversionAbort, s.handleCleanupMessage)
	s.subscribeTopic(ctx, TopicReversionError, s.handleCleanupMessage)

	// Publish the initial candidate event
	startEvt := CandidateFoundEvent{
		Flow:       FlowReversion,
		Symbol:     candidate.Symbol,
		Candidate:  candidate,
		SettleTime: settleTime,
		SendNotify: false,
		Timestamp:  s.deps.Clock.Now(),
	}

	if err := s.publishEvent(ctx, TopicReversionCandidate, startEvt); err != nil {
		return err
	}

	// Block until flow completes or is aborted
	select {
	case <-ctx.Done():
		s.abort(ctx, "context canceled")
		return ctx.Err()
	case <-s.doneChan:
	}

	applogger.WithCtx(ctx, s.log).Info("🏁 Event-driven reversion bot lifecycle completed", slog.String("symbol", candidate.Symbol))
	return nil
}

func (s *Strategy) handleArmMessage(ctx context.Context, msg *message.Message) error {
	var evt CandidateFoundEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return s.handleArm(ctx, evt)
}

func (s *Strategy) handleWaitMessage(ctx context.Context, msg *message.Message) error {
	var evt ArmedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return s.handleWait(ctx, evt)
}

func (s *Strategy) handleRecheckMessage(ctx context.Context, msg *message.Message) error {
	var evt WaitCompleteEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return s.handleRecheck(ctx, evt)
}

func (s *Strategy) handleFireIOCMessage(ctx context.Context, msg *message.Message) error {
	var evt ConfirmedEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return s.handleFireIOC(ctx, evt)
}

func (s *Strategy) handleIOCFiredMessage(ctx context.Context, msg *message.Message) error {
	var evt IOCFiredEvent
	if err := json.Unmarshal(msg.Payload, &evt); err != nil {
		return err
	}
	return s.handleIOCFired(ctx, evt)
}

func (s *Strategy) handleCleanupMessage(ctx context.Context, msg *message.Message) error {
	return s.handleCleanup(ctx, msg.UUID, msg.Metadata)
}
