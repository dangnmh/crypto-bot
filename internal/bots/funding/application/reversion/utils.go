package reversion

import (
	"context"
	"log/slog"
	"reflect"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill"
)

const (
	reversionReasonNoFill        = "no_fill"
	reversionMethodFallbackClose = "fallback_close"
)

// Strategy implements strategy.Strategy interface in a lightweight, stateless manner.
type Strategy struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   application.Deps
	log    *slog.Logger
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

func (s *Strategy) Enabled(cfg config.SymbolConfig) bool {
	return cfg.FundingReversion.Enabled
}

func (s *Strategy) Execute(ctx context.Context, settleTime time.Time, candidate domain.Candidate) error {
	ctx = observability.WithReversionID(ctx)

	// Ensure global subscriptions are registered (lazy-loaded if not initialized at startup, e.g. in tests)
	InitGlobalSubscriptions(ctx, s.deps, s.global)

	s.log.InfoContext(ctx, "🚀 Triggering event-driven reversion bot lifecycle execution", slog.String("symbol", candidate.Symbol))

	startEvt := CandidateFoundEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:       FlowReversion,
			ReqID:      observability.ReversionID(ctx),
			Symbol:     candidate.Symbol,
			Exchange:   candidate.Config.Exchange,
			SendNotify: false,
			Timestamp:  s.deps.Clock.Now(),
			EventID:    watermill.NewUUID(),
			Seq:        1,
			Topic:      TopicReversionCandidate,
		},
		Candidate:  candidate,
		SettleTime: settleTime,
	}

	return s.deps.EventBus.Publish(TopicReversionCandidate, startEvt)
}

func (s *Strategy) CleanupOpenExposure(ctx context.Context) error {
	err := s.deps.Client.CloseAllPositions(ctx, s.cfg.Symbol)
	if err != nil {
		s.log.ErrorContext(ctx, "Reversion fallback close all failed during cleanup",
			slog.Any("error", err),
			slog.String("symbol", s.cfg.Symbol),
		)
	}
	return err
}

// StatelessRunner handles global, single-instance reversion event subscriptions.
type StatelessRunner struct {
	deps      application.Deps
	globalCfg *config.Config
	bus       *eventbus.Bus
	log       *slog.Logger
}

func (r *StatelessRunner) getSymbolConfig(symbol string) (config.SymbolConfig, bool) {
	for i := range r.globalCfg.Symbols {
		if r.globalCfg.Symbols[i].Symbol == symbol {
			return r.globalCfg.Symbols[i], true
		}
	}
	return config.SymbolConfig{}, false
}

func (r *StatelessRunner) publishEvent(ctx context.Context, topic string, payload any) error {
	if r.bus == nil {
		return nil
	}

	payload = stampEventTrace(topic, payload)
	r.log.InfoContext(ctx, "Reversion: Publishing event", slog.String("topic", topic), slog.Any("payload", payload))

	if err := r.bus.Publish(topic, payload); err != nil {
		r.log.ErrorContext(ctx, "Failed to publish event", slog.String("topic", topic), slog.Any("error", err))
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
			Timestamp: r.deps.Clock.Now(),
		}

		go func() {
			_ = r.deps.Notifier.Send(ctx, evt)
		}()
	}

	return nil
}

func stampEventTrace(topic string, payload any) any {
	copyVal, base, ok := mutableBaseReversionEvent(payload)
	if !ok {
		return payload
	}

	setStringFieldIfEmpty(base, "EventID", watermill.NewUUID())
	setStringField(base, "Topic", topic)

	return copyVal.Interface()
}

func mutableBaseReversionEvent(payload any) (reflect.Value, reflect.Value, bool) {
	v := reflect.ValueOf(payload)
	if !v.IsValid() {
		return reflect.Value{}, reflect.Value{}, false
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return reflect.Value{}, reflect.Value{}, false
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return reflect.Value{}, reflect.Value{}, false
	}

	copyVal := reflect.New(v.Type()).Elem()
	copyVal.Set(v)
	base := copyVal.FieldByName("BaseReversionEvent")
	if !base.IsValid() || !base.CanSet() {
		return reflect.Value{}, reflect.Value{}, false
	}
	return copyVal, base, true
}

func setStringField(base reflect.Value, name, value string) {
	field := base.FieldByName(name)
	if field.IsValid() && field.CanSet() {
		field.SetString(value)
	}
}

func setStringFieldIfEmpty(base reflect.Value, name, value string) {
	field := base.FieldByName(name)
	if field.IsValid() && field.CanSet() && field.String() == "" {
		field.SetString(value)
	}
}

func nextReversionBase(prev BaseReversionEvent, symbol string, timestamp time.Time) BaseReversionEvent {
	seq := int64(0)
	if prev.Seq > 0 {
		seq = prev.Seq + 1
	}
	return BaseReversionEvent{
		Flow:          FlowReversion,
		ReqID:         prev.ReqID,
		Symbol:        symbol,
		Exchange:      prev.Exchange,
		Timestamp:     timestamp,
		Seq:           seq,
		PreviousTopic: prev.Topic,
	}
}

func nextNotifyReversionBase(prev BaseReversionEvent, symbol string, timestamp time.Time) BaseReversionEvent {
	base := nextReversionBase(prev, symbol, timestamp)
	base.SendNotify = true
	return base
}

func (r *StatelessRunner) WaitUntil(ctx context.Context, symbol string, target time.Time) bool {
	if d := r.deps.Clock.Until(target); d > 0 {
		r.log.DebugContext(ctx, "⏱️ wait", slog.String("symbol", symbol), slog.Time("target", target), slog.Duration("wait", d))
		return r.deps.Clock.Sleep(ctx, d) == nil
	}
	return ctx.Err() == nil
}

func (r *StatelessRunner) subscribeWS(ctx context.Context, symbol string) error {
	return r.deps.WsSub.SubscribeTicker(ctx, symbol)
}

func (r *StatelessRunner) unsubscribeWS(ctx context.Context, symbol string) {
	if err := r.deps.WsSub.UnsubscribeTicker(ctx, symbol); err != nil {
		r.log.WarnContext(ctx, "⚠️ Failed to unsubscribe ticker", slog.String("symbol", symbol), slog.Any("error", err))
	}
}

func (r *StatelessRunner) refreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := r.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
	return err
}

func (r *StatelessRunner) abort(ctx context.Context, symbol, reqID, exchangeName, reason string) {
	evt := AbortEvent{
		BaseReversionEvent: BaseReversionEvent{
			Flow:      FlowReversion,
			ReqID:     reqID,
			Symbol:    symbol,
			Exchange:  exchangeName,
			Timestamp: r.deps.Clock.Now(),
		},
		Reason: reason,
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, evt)
}

func (r *StatelessRunner) abortAfter(ctx context.Context, prev BaseReversionEvent, symbol, reason string) {
	evt := AbortEvent{
		BaseReversionEvent: nextReversionBase(prev, symbol, r.deps.Clock.Now()),
		Reason:             reason,
	}
	_ = r.publishEvent(ctx, TopicReversionAbort, evt)
}

func (r *StatelessRunner) RetryWithBackoff(ctx context.Context, attempts int, fn func() error) (int, error) {
	return r.RetryWithBackoffOpts(ctx, attempts, 100*time.Millisecond, 5*time.Second, fn)
}

func (r *StatelessRunner) RetryWithBackoffOpts(ctx context.Context, attempts int, baseDelay, maxDelay time.Duration, fn func() error) (int, error) {
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
		if sleepErr := r.deps.Clock.Sleep(ctx, delayWithJitter); sleepErr != nil {
			return i, err
		}
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return attempts, err
}

func (r *StatelessRunner) handlePositionUpdate(ctx context.Context, pos exchange.PersonalPositionUpdate, prev BaseReversionEvent) {
	r.log.Debug("Position update received", slog.Any("pos", pos))

	fillPrice := pos.OpenAvgPrice
	if fillPrice == 0 {
		fillPrice = pos.HoldAvgPrice
	}
	if fillPrice == 0 {
		if pd, err := r.deps.PriceStore.GetPrice(ctx, pos.Symbol, 5*time.Second); err == nil {
			fillPrice = pd.LastPrice
		}
	}

	side := shared.SideOpenLong
	closeSide := shared.SideCloseLong
	if pos.PositionType == 2 { // Short position
		side = shared.SideOpenShort
		closeSide = shared.SideCloseShort
	}

	if pos.HoldVol > 0 {
		evt := OrderFilledEvent{
			BaseReversionEvent: nextNotifyReversionBase(prev, pos.Symbol, r.deps.Clock.Now()),
			OrderID:            "", // Stateless matches purely by symbol
			Side:               side,
			CloseSide:          closeSide,
			FillPrice:          fillPrice,
			FillVol:            pos.HoldVol,
		}
		go func() {
			_ = r.publishEvent(ctx, TopicReversionOrderFilled, evt)
		}()
	} else if pos.HoldVol == 0 {
		evt := PositionClosedEvent{
			BaseReversionEvent: nextNotifyReversionBase(prev, pos.Symbol, r.deps.Clock.Now()),
			EntryPrice:         fillPrice,
			ClosePrice:         fillPrice, // Fallback exit price matches open avg if no specific close avg price exists
			CloseVol:           pos.CloseVol,
			Reason:             "exchange_push",
			GrossProfit:        pos.CloseProfitLoss,
			NetProfit:          pos.CloseProfitLoss - pos.Fee,
			Fee:                pos.Fee,
			HoldFee:            pos.HoldFee,
			Direction:          side,
			Method:             "watcher",
		}
		if pos.CloseAvgPrice > 0 {
			evt.ClosePrice = pos.CloseAvgPrice
		}
		go func() {
			_ = r.publishEvent(ctx, TopicReversionPositionClosed, evt)
		}()
	}
}
