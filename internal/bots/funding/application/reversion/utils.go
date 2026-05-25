package reversion

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"

	"github.com/ThreeDotsLabs/watermill/message"
)

const (
	reversionReasonNoFill        = "no_fill"
	reversionMethodFallbackClose = "fallback_close"
)

type Strategy struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   application.Deps
	log    *slog.Logger

	settleTime time.Time
	mu         sync.Mutex
	candidate  domain.Candidate
	order      *application.OrderRef
	fill       *application.FillInfo
	terminal   bool

	bus      *eventbus.Bus
	doneChan chan struct{}

	initialPosition *exchange.PersonalPositionUpdate
	latestPosition  *exchange.PersonalPositionUpdate
}

func NewStrategy(
	cfg config.SymbolConfig,
	global *config.Config,
	deps application.Deps,
) *Strategy {
	logger := deps.Log.With("flow", FlowReversion)
	return &Strategy{
		cfg:    cfg,
		global: global,
		deps:   deps,
		log:    logger,
	}
}

var _ strategy.Strategy = (*Strategy)(nil)

func (s *Strategy) Flow() string {
	return FlowReversion
}

func (s *Strategy) Enabled(config.SymbolConfig) bool {
	return true
}

func (s *Strategy) Execute(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error {
	return s.executeInternal(ctx, settleTime, candidate)
}

func (s *Strategy) publishEvent(ctx context.Context, topic string, payload any) error {
	s.mu.Lock()
	bus := s.bus
	s.mu.Unlock()

	if bus == nil {
		return nil
	}

	if err := bus.Publish(topic, payload); err != nil {
		s.log.Error("Failed to publish event", slog.String("topic", topic), slog.Any("error", err))
		return err
	}

	// Check if the event wants to trigger a notification
	if revEvt, ok := payload.(ReversionEvent); ok && revEvt.ShouldNotify() {
		level := notifier.LevelTrading
		if topic == TopicReversionAbort || topic == TopicReversionError {
			level = notifier.LevelCritical
		}

		evt := notifier.Event{
			Level:     level,
			Symbol:    revEvt.GetSymbol(),
			Message:   revEvt.GetMessage(),
			Data:      revEvt.GetDataMap(),
			Timestamp: s.deps.Clock.Now(),
		}

		go func() {
			_ = s.deps.Notifier.Send(ctx, evt)
		}()
	}

	return nil
}

func (s *Strategy) subscribeTopic(ctx context.Context, topic string, handler func(context.Context, *message.Message) error) {
	s.mu.Lock()
	bus := s.bus
	s.mu.Unlock()

	if bus == nil {
		return
	}

	ch, err := bus.Subscribe(ctx, topic)
	if err != nil {
		applogger.WithCtx(ctx, s.log).Error("Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if err := handler(ctx, msg); err != nil {
					applogger.WithCtx(ctx, s.log).Error("Handler execution failed", slog.String("topic", topic), slog.Any("error", err))
				}
				msg.Ack()
			}
		}
	}()
}

func (s *Strategy) WaitUntil(ctx context.Context, target time.Time) bool {
	if d := s.deps.Clock.Until(target); d > 0 {
		applogger.WithCtx(ctx, s.log).Debug("⏱️ wait", slog.Time("target", target), slog.Duration("wait", d))
		return s.deps.Clock.Sleep(ctx, d) == nil
	}
	return ctx.Err() == nil
}

func (s *Strategy) isTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.terminal
}

func (s *Strategy) tryMarkTerminal() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.terminal {
		return false
	}
	s.terminal = true
	return true
}

func (s *Strategy) getFill() (application.FillInfo, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fill == nil {
		return application.FillInfo{}, false
	}
	return *s.fill, true
}

func (s *Strategy) getCandidateCopy() domain.Candidate {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.candidate
}

func (s *Strategy) setCandidate(c domain.Candidate) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.candidate = c
}

func (s *Strategy) subscribeWS(ctx context.Context) error {
	return s.deps.WsSub.SubscribeTicker(ctx, s.cfg.Symbol)
}

func (s *Strategy) unsubscribeWS(ctx context.Context) {
	if err := s.deps.WsSub.UnsubscribeTicker(ctx, s.cfg.Symbol); err != nil {
		applogger.WithCtx(ctx, s.log).Warn("⚠️ Failed to unsubscribe ticker", slog.String("symbol", s.cfg.Symbol), slog.Any("error", err))
	}
}

func (s *Strategy) refreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := s.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
	return err
}

func (s *Strategy) abort(ctx context.Context, reason string) {
	if s.tryMarkTerminal() {
		evt := AbortEvent{
			Flow:      FlowReversion,
			Symbol:    s.cfg.Symbol,
			Reason:    reason,
			Timestamp: s.deps.Clock.Now(),
		}
		_ = s.publishEvent(ctx, TopicReversionAbort, evt)
	}
}

func (s *Strategy) RetryWithBackoff(ctx context.Context, attempts int, fn func() error) (int, error) {
	return s.RetryWithBackoffOpts(ctx, attempts, 100*time.Millisecond, 5*time.Second, fn)
}

func (s *Strategy) RetryWithBackoffOpts(ctx context.Context, attempts int, baseDelay, maxDelay time.Duration, fn func() error) (int, error) {
	if attempts <= 0 {
		attempts = 1
	}
	var err error
	delay := baseDelay
	for i := 1; i <= attempts; i++ {
		if err = fn(); err == nil {
			return i, nil
		}
		if i == attempts {
			break
		}
		jitter := delay * 20 / 100
		delayWithJitter := delay + time.Duration((float64(delay)-float64(jitter))*0.5+float64(jitter)*0.5)
		if sleepErr := s.deps.Clock.Sleep(ctx, delayWithJitter); sleepErr != nil {
			return i, err
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return attempts, err
}

func (s *Strategy) handlePositionUpdate(ctx context.Context, pos exchange.PersonalPositionUpdate) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.log.Debug("Position update received", slog.Any("pos", pos))
	if s.order == nil {
		return
	}

	s.latestPosition = &pos

	if s.fill == nil && pos.HoldVol > 0 {
		s.initialPosition = &pos

		fillPrice := pos.OpenAvgPrice
		if fillPrice == 0 {
			fillPrice = pos.HoldAvgPrice
		}
		if fillPrice == 0 {
			fillPrice = s.candidate.LastPrice
		}
		s.fill = &application.FillInfo{
			Symbol:    s.cfg.Symbol,
			OrderID:   s.order.OrderID,
			FillPrice: fillPrice,
			FillVol:   pos.HoldVol,
		}

		evt := OrderFilledEvent{
			Flow:       FlowReversion,
			Symbol:     s.cfg.Symbol,
			OrderID:    s.order.OrderID,
			Side:       s.candidate.Side,
			CloseSide:  s.candidate.CloseSide,
			FillPrice:  fillPrice,
			FillVol:    pos.HoldVol,
			Timestamp:  s.deps.Clock.Now(),
			SendNotify: true,
		}
		go func() {
			_ = s.publishEvent(ctx, TopicReversionOrderFilled, evt)
		}()
	}

	if s.fill != nil && pos.HoldVol == 0 {
		closePrice := pos.CloseAvgPrice
		if closePrice == 0 {
			closePrice = pos.HoldAvgPrice
		}
		if closePrice == 0 {
			closePrice = s.candidate.LastPrice
		}

		evt := PositionClosedEvent{
			Flow:       FlowReversion,
			Symbol:     s.cfg.Symbol,
			ClosePrice: closePrice,
			CloseVol:   s.fill.FillVol,
			Reason:     "exchange_push",
			Method:     "watcher",
			Timestamp:  s.deps.Clock.Now(),
			SendNotify: true,
		}
		go func() {
			_ = s.publishEvent(ctx, TopicReversionPositionClosed, evt)
		}()
	}
}

func (s *Strategy) CleanupOpenExposure(ctx context.Context) error {
	s.mu.Lock()
	if s.order == nil {
		s.mu.Unlock()
		return nil
	}
	sym := s.order.Symbol
	s.mu.Unlock()

	_, err := s.forceClosePosition(ctx, sym, 3)
	return err
}
