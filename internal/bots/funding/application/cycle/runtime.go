package cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/notifier"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"
	applogger "crypto-bot/pkg/logger"
	"crypto-bot/pkg/tracectx"

	"github.com/ThreeDotsLabs/watermill/message"
)

const notifierSendTimeout = 5 * time.Second

// Deps holds all external dependencies for a funding cycle.
type Deps struct {
	Client        exchange.Client
	WsSub         ws.Subscriber
	OrderNotifier watcher.OrderNotifier
	TickerStore   store.TickerReader
	ContractStore store.ContractReader
	PriceStore    store.PriceReader
	FundingStore  store.FundingReader
	DepthStore    store.DepthReader
	Clock         shared.Clock
	Log           *slog.Logger
	Notifier      notifier.Notifier
}

// Runtime owns shared per-cycle state and dependencies used by flow packages.
type Runtime struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   Deps
	subs   *SubscriptionManager

	bus       *eventbus.Bus
	mu        sync.Mutex
	candidate domain.Candidate
	results   []any

	reqID    string
	settle   time.Time
	eventSeq int64
	eventLog []events.JournalEnvelope

	terminal map[string]bool

	excursionCancel context.CancelFunc

	reversionOrderID string
	reversionFill    *events.OrderFilledEvent
	trapOrder        *events.TrapFiredEvent
	trapFill         *events.OrderFilledEvent
	trapTerminal     bool
}

func NewRuntime(cfg config.SymbolConfig, global *config.Config, deps Deps) *Runtime {
	return &Runtime{
		cfg:    cfg,
		global: global,
		deps:   deps,
		subs: NewSubscriptionManager(
			deps.WsSub,
			cfg.Symbol,
			deps.Log,
		),
	}
}

func (r *Runtime) Begin(ctx context.Context, reqID string, settle time.Time, log *slog.Logger) {
	r.BeginWithContext(ctx, reqID, settle, log)
}

func (r *Runtime) BeginWithContext(ctx context.Context, reqID string, settle time.Time, log *slog.Logger) {
	r.deps.Log = log
	r.bus = eventbus.New(log)
	r.terminal = make(map[string]bool)
	r.reqID = reqID
	r.settle = settle
	r.eventSeq = 0
	r.eventLog = make([]events.JournalEnvelope, 0, 32)
	cfgJSON, _ := json.Marshal(ToTradeConfig(r.cfg))
	r.RecordAndPublishCtx(ctx, reqID, events.TopicCycleStarted, events.CycleStartEvent{
		Symbol:     r.cfg.Symbol,
		SettleTime: settle,
		Config:     cfgJSON,
	})
}

// GetReqID returns the current request ID.
func (r *Runtime) GetReqID() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reqID
}

func (r *Runtime) CloseBus() error {
	if r.bus == nil {
		return nil
	}
	return r.bus.Close()
}

func (r *Runtime) DumpTimeline(log *slog.Logger) {
	if r.bus != nil {
		r.bus.DumpTimeline(log)
	}
}

func (r *Runtime) Config() config.SymbolConfig {
	return r.cfg
}

func (r *Runtime) Global() *config.Config {
	return r.global
}

func (r *Runtime) Deps() *Deps {
	return &r.deps
}

func (r *Runtime) Log() *slog.Logger {
	return r.deps.Log
}

func (r *Runtime) JourneyEvents() []events.JournalEnvelope {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := make([]events.JournalEnvelope, len(r.eventLog))
	for i := range r.eventLog {
		result[i] = r.eventLog[i]
		result[i].Payload = append(json.RawMessage(nil), r.eventLog[i].Payload...)
	}
	return result
}

// RecordAndPublish records an event to the event journal and publishes it to the event bus.
func (r *Runtime) RecordAndPublish(ctx context.Context, reqID, topic string, payload any) {
	r.RecordAndPublishCtx(ctx, reqID, topic, payload)
}

// RecordAndPublishCtx records an event to the event journal and publishes it to the event bus.
func (r *Runtime) RecordAndPublishCtx(ctx context.Context, reqID, topic string, payload any) {
	r.mu.Lock()
	r.eventSeq++
	rawPayload, _ := json.Marshal(payload)

	env := events.JournalEnvelope{
		Seq:        r.eventSeq,
		Time:       time.Now(),
		ReqID:      reqID,
		Symbol:     r.cfg.Symbol,
		SettleTime: r.settle,
		Flow:       r.getFlowFromTopic(topic),
		Topic:      topic,
		Payload:    rawPayload,
	}
	r.eventLog = append(r.eventLog, env)
	r.mu.Unlock()

	if err := r.bus.Publish(topic, payload); err != nil {
		applogger.WithCtx(ctx, r.deps.Log).Error("Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}

	if r.deps.Notifier != nil {
		if r.shouldNotify(topic, payload) {
			msg := r.buildNotificationMessage(topic, payload)
			notifyCtx := context.WithoutCancel(r.contextWithReqID(ctx, reqID))
			go func() {
				sendCtx, cancel := context.WithTimeout(notifyCtx, notifierSendTimeout)
				defer cancel()
				_ = r.deps.Notifier.Send(sendCtx, msg)
			}()
		}
	}
}

func (r *Runtime) contextWithReqID(ctx context.Context, reqID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if reqID == "" || tracectx.ReqID(ctx) != "" {
		return ctx
	}
	return tracectx.WithReqID(ctx, reqID)
}

func (r *Runtime) shouldNotify(topic string, payload any) bool {
	if n, ok := payload.(events.Notifiable); ok && n.ShouldNotify() {
		return true
	}

	switch topic {
	case events.TopicCycleFinalPnL,
		events.TopicReversionOrderFilled,
		events.TopicTrapOrderFilled,
		events.TopicScanAbort,
		events.TopicReversionAbort,
		events.TopicTrapAbort,
		events.TopicReversionError,
		events.TopicTrapError,
		events.TopicSymbolDisabled,
		events.TopicOrderRejected:
		return true
	default:
		return false
	}
}

//nolint:cyclop // Formatting important trading notifications is intentionally centralized by topic.
func (r *Runtime) buildNotificationMessage(topic string, payload any) notifier.Event {
	evt := notifier.Event{
		Symbol:    r.cfg.Symbol,
		Timestamp: time.Now(),
	}

	switch topic {
	case events.TopicCycleFinalPnL:
		evt.Level = notifier.LevelTrading
		switch pnl := payload.(type) {
		case events.FinalPnLEvent:
			evt.Message = fmt.Sprintf("💰 Cycle Completed\nNet PnL: %.4f USDT\nFees: %.4f\nEvents: %d", pnl.NetPnL, pnl.TradingFees, pnl.EventCount)
		case *events.FinalPnLEvent:
			evt.Message = fmt.Sprintf("💰 Cycle Completed\nNet PnL: %.4f USDT\nFees: %.4f\nEvents: %d", pnl.NetPnL, pnl.TradingFees, pnl.EventCount)
		}
	case events.TopicReversionOrderFilled, events.TopicTrapOrderFilled:
		evt.Level = notifier.LevelTrading
		switch fill := payload.(type) {
		case events.OrderFilledEvent:
			evt.Message = fmt.Sprintf("✅ Order Filled\nPrice: %.4f\nVol: %.4f\nProfit: %.4f", fill.FillPrice, fill.FillVol, fill.Profit)
		case *events.OrderFilledEvent:
			evt.Message = fmt.Sprintf("✅ Order Filled\nPrice: %.4f\nVol: %.4f\nProfit: %.4f", fill.FillPrice, fill.FillVol, fill.Profit)
		}
	case events.TopicScanAbort, events.TopicReversionAbort, events.TopicTrapAbort:
		evt.Level = notifier.LevelTrading
		switch abort := payload.(type) {
		case events.CycleAbortEvent:
			evt.Message = fmt.Sprintf("⚠️ Cycle Aborted\nReason: %s", abort.Reason)
		case *events.CycleAbortEvent:
			evt.Message = fmt.Sprintf("⚠️ Cycle Aborted\nReason: %s", abort.Reason)
		}
	case events.TopicReversionError, events.TopicTrapError:
		evt.Level = notifier.LevelCritical
		switch errEvt := payload.(type) {
		case events.CycleErrorEvent:
			evt.Message = fmt.Sprintf("❌ Cycle Error\nError: %s", errEvt.Error)
		case *events.CycleErrorEvent:
			evt.Message = fmt.Sprintf("❌ Cycle Error\nError: %s", errEvt.Error)
		}
	case events.TopicSymbolDisabled:
		evt.Level = notifier.LevelCritical
		switch disabled := payload.(type) {
		case events.SymbolDisabledEvent:
			evt.Message = fmt.Sprintf("🚫 Symbol Disabled\nReason: %s\nSource: %s", disabled.Reason, disabled.Source)
		case *events.SymbolDisabledEvent:
			evt.Message = fmt.Sprintf("🚫 Symbol Disabled\nReason: %s\nSource: %s", disabled.Reason, disabled.Source)
		}
	case events.TopicOrderRejected:
		evt.Level = notifier.LevelCritical
		switch rejected := payload.(type) {
		case events.WSOrderRejectedEvent:
			evt.Message = fmt.Sprintf("🚨 Order Rejected\nError: %s", rejected.Error)
		case *events.WSOrderRejectedEvent:
			evt.Message = fmt.Sprintf("🚨 Order Rejected\nError: %s", rejected.Error)
		}
	default:
		evt.Level = notifier.LevelInfo
		evt.Message = fmt.Sprintf("Event: %s", topic)
	}

	return evt
}

func (r *Runtime) getFlowFromTopic(topic string) string {
	if strings.Contains(topic, "reversion") {
		return events.FlowReversion
	}
	if strings.Contains(topic, "trap") {
		return events.FlowTrap
	}
	if strings.Contains(topic, "scan") {
		return events.FlowScan
	}
	return events.FlowCycle
}

func (r *Runtime) Subscribe(ctx context.Context, topic string, handler func(*message.Message)) {
	msgs, err := r.bus.Subscribe(ctx, topic)
	if err != nil {
		applogger.WithCtx(ctx, r.deps.Log).Error("Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
		return
	}
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-msgs:
				if !ok {
					return
				}
				msg.Ack()
				handler(msg)
			}
		}
	}()
}

func (r *Runtime) Publish(ctx context.Context, topic string, payload any) {
	r.PublishCtx(ctx, topic, payload)
}

func (r *Runtime) PublishCtx(ctx context.Context, topic string, payload any) {
	if err := r.bus.Publish(topic, payload); err != nil {
		applogger.WithCtx(ctx, r.deps.Log).Error("🔴 Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}
}

func (r *Runtime) PublishStart(settle time.Time) error {
	return r.bus.Publish(events.TopicScanStart, events.CycleStartEvent{
		Symbol:     r.cfg.Symbol,
		SettleTime: settle,
	})
}

func (r *Runtime) Abort(ctx context.Context, reqID, source, reason string) {
	r.AbortCtx(ctx, reqID, source, reason)
}

func (r *Runtime) AbortCtx(ctx context.Context, reqID, source, reason string) {
	if source == "scan" {
		r.RecordAndPublishCtx(ctx, reqID, events.TopicScanCandidateFound, events.CandidateFoundEvent{
			Flow:   events.FlowScan,
			Symbol: r.cfg.Symbol,
		}) // placeholder for scan rejected
		r.PublishCtx(ctx, events.TopicScanAbort, events.CycleAbortEvent{
			Symbol: r.cfg.Symbol,
			Reason: reason,
		})
	}
	if !r.TryMarkFlowTerminal(events.FlowReversion) {
		return
	}
	r.RecordAndPublishCtx(ctx, reqID, events.TopicReversionAbort, events.CycleAbortEvent{
		Flow:   events.FlowReversion,
		Symbol: r.cfg.Symbol,
		Reason: reason,
	})
}

func (r *Runtime) CandidateCopy() domain.Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.candidate
}

func (r *Runtime) SetCandidate(c domain.Candidate) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.candidate = c
}

func (r *Runtime) UpdateCandidate(fn func(*domain.Candidate)) domain.Candidate {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(&r.candidate)
	return r.candidate
}

func (r *Runtime) AppendResult(result any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.results = append(r.results, result)
}

func (r *Runtime) SettleTime() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.settle
}

func (r *Runtime) MarkReversionOrder(orderID string) {
	if orderID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reversionOrderID = orderID
}

func (r *Runtime) MarkReversionFill(evt events.OrderFilledEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fill := evt
	r.reversionFill = &fill
}

func (r *Runtime) ReversionFill() (events.OrderFilledEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.reversionFill == nil {
		return events.OrderFilledEvent{}, false
	}
	return *r.reversionFill, true
}

func (r *Runtime) MarkTrapOrder(evt events.TrapFiredEvent) {
	if evt.OrderID == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	trapOrder := evt
	r.trapOrder = &trapOrder
	r.trapTerminal = false
}

func (r *Runtime) MarkTrapFill(evt events.OrderFilledEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fill := evt
	r.trapFill = &fill
}

func (r *Runtime) MarkTrapTerminal() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trapTerminal = true
}

func (r *Runtime) TrapSnapshot() (order events.TrapFiredEvent, hasOrder bool, fill events.OrderFilledEvent, hasFill, terminal bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.trapOrder != nil {
		order = *r.trapOrder
		hasOrder = true
	}
	if r.trapFill != nil {
		fill = *r.trapFill
		hasFill = true
	}
	return order, hasOrder, fill, hasFill, r.trapTerminal
}

func (r *Runtime) SubscribeAll(ctx context.Context) error {
	return r.subs.SubscribeAll(ctx)
}

func (r *Runtime) UnsubscribeAll(ctx context.Context) {
	r.subs.UnsubscribeAll(ctx)
}

func (r *Runtime) BuildCandidate(td *store.TickerData) domain.Candidate {
	intent := domain.TradeIntent{
		Symbol:      td.Symbol,
		FundingRate: td.FundingRate,
	}
	if td.FundingRate > 0 {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenLong, shared.SideCloseLong, "bestAsk"
	} else {
		intent.Side, intent.CloseSide, intent.RefPriceType = shared.SideOpenShort, shared.SideCloseShort, "bestBid"
	}

	return domain.Candidate{
		Config:      ToTradeConfig(r.cfg),
		TradeIntent: intent,
		MarketData: domain.MarketData{
			LastPrice: td.LastPrice,
			BestBid:   td.BestBid,
			BestAsk:   td.BestAsk,
			Volume24:  td.Volume24,
			Amount24:  td.Amount24,
		},
	}
}

func (r *Runtime) Enrich(ctx context.Context, c *domain.Candidate) bool {
	cd, err := r.deps.ContractStore.GetContract(ctx, c.Symbol)
	if err != nil {
		applogger.WithCtx(ctx, r.deps.Log).Warn("🟡 No contract data — skip")
		return false
	}
	c.ContractSpec = domain.ContractSpec{
		PriceUnit:    cd.PriceUnit,
		VolUnit:      cd.VolUnit,
		MinVol:       cd.MinVol,
		PriceScale:   cd.PriceScale,
		VolScale:     cd.VolScale,
		ContractSize: cd.ContractSize,
		TakerFeeRate: cd.TakerFeeRate,
		MakerFeeRate: cd.MakerFeeRate,
	}
	return true
}

func (r *Runtime) RefreshPrice(ctx context.Context, c *domain.Candidate) error {
	pd, err := r.deps.PriceStore.GetPrice(ctx, c.Symbol, 5*time.Second)
	if err == nil {
		c.BestBid, c.BestAsk, c.LastPrice = pd.BestBid, pd.BestAsk, pd.LastPrice
	}
	return err
}

func (r *Runtime) WaitUntil(ctx context.Context, target time.Time) bool {
	if d := r.deps.Clock.Until(target); d > 0 {
		applogger.WithCtx(ctx, r.deps.Log).Debug("⏱️ wait", slog.Time("target", target), slog.Duration("wait", d))
		return r.deps.Clock.Sleep(ctx, d) == nil
	}
	return ctx.Err() == nil
}

func (r *Runtime) Sleep(ctx context.Context, d time.Duration) bool {
	err := r.deps.Clock.Sleep(ctx, d)
	return err == nil
}

func (r *Runtime) RetryWithBackoff(ctx context.Context, attempts int, fn func() error) (int, error) {
	return r.RetryWithBackoffOpts(ctx, attempts, 100*time.Millisecond, 5*time.Second, fn)
}

// RetryWithBackoffOpts performs retry with exponential backoff and jitter.
// baseDelay: initial delay (e.g., 100ms)
// maxDelay: cap for exponential backoff (e.g., 5s)
// Returns (actual_retry_count, error).
func (r *Runtime) RetryWithBackoffOpts(ctx context.Context, attempts int, baseDelay, maxDelay time.Duration, fn func() error) (int, error) {
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
		// Add jitter: ±20% of delay to avoid thundering herd
		jitter := delay * 20 / 100
		delayWithJitter := delay + time.Duration((float64(delay)-float64(jitter))*0.5+float64(jitter)*0.5)
		if sleepErr := r.deps.Clock.Sleep(ctx, delayWithJitter); sleepErr != nil {
			return i, err
		}
		// Exponential backoff: double delay each retry, capped at maxDelay
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
	return attempts, err
}

func (r *Runtime) StartExcursionPriceStream(ctx context.Context, reqID string) {
	if r.deps.PriceStore == nil || r.excursionCancel != nil {
		return
	}

	streamCtx, cancel := context.WithCancel(ctx)
	r.excursionCancel = cancel
	updates := r.deps.PriceStore.SubscribePrice(streamCtx, r.cfg.Symbol)

	go func() {
		for pd := range updates {
			if pd == nil || pd.LastPrice <= 0 {
				continue
			}
			r.mu.Lock()
			r.eventSeq++
			env := events.JournalEnvelope{
				Seq:     r.eventSeq,
				Time:    pd.UpdatedAt,
				ReqID:   reqID,
				Symbol:  r.cfg.Symbol,
				Flow:    events.FlowCycle,
				Topic:   events.TopicExcursionPriceObserved,
				Payload: []byte(fmt.Sprintf(`{"price":%f,"time":%q}`, pd.LastPrice, pd.UpdatedAt.Format(time.RFC3339))),
			}
			r.eventLog = append(r.eventLog, env)
			r.mu.Unlock()
		}
	}()
}

func (r *Runtime) StopExcursionPriceStream() {
	if r.excursionCancel != nil {
		r.excursionCancel()
		r.excursionCancel = nil
	}
}

// SubscribeWSOrderEvents subscribes to order lifecycle events from OrderNotifier
// and records them via RecordAndPublish for event sourcing.
func (r *Runtime) SubscribeWSOrderEvents(ctx context.Context, reqID, symbol string) {
	positionTimeout := r.positionUpdateWatchTimeout()

	// Subscribe to position updates
	r.deps.OrderNotifier.OnPositionUpdate(ctx, symbol, positionTimeout, func(pos exchange.PersonalPositionUpdate) {
		r.RecordAndPublishCtx(ctx, reqID, events.TopicPositionUpdated, events.WSPositionUpdatedEvent{
			Symbol:        pos.Symbol,
			Side:          pos.PositionType,
			Size:          pos.HoldVol,
			EntryPrice:    pos.HoldAvgPrice,
			MarkPrice:     pos.LiquidatePrice,
			UnrealizedPnL: pos.Realized,
			Leverage:      pos.Leverage,
			Timestamp:     time.Now(),
		})
		r.publishPositionClosedFromUpdate(ctx, reqID, pos)
	})

	// Subscribe to track order updates
	r.deps.OrderNotifier.OnTrackOrderUpdate(ctx, "", "", 30*time.Second, func(track exchange.PersonalTrackOrderUpdate) {
		var orderID string
		if oid, ok := track.OrderID.(string); ok {
			orderID = oid
		}
		var id string
		if iid, ok := track.ID.(string); ok {
			id = iid
		}
		r.RecordAndPublishCtx(ctx, reqID, events.TopicTrackUpdated, events.WSTrackUpdatedEvent{
			TrackID:     id,
			OrderID:     orderID,
			Symbol:      track.Symbol,
			ActivePrice: track.ActivePrice,
			CallbackPct: float64(track.BackValue) / 100.0,
			Status:      trackStateToString(track.State),
			Timestamp:   time.Now(),
		})
	})
}

func (r *Runtime) positionUpdateWatchTimeout() time.Duration {
	timeout := 30 * time.Second
	configured := timeout
	if d := time.Duration(r.cfg.FundingReversion.PostSettleTimeout); d > timeout {
		configured = d
	}
	if d := time.Duration(r.cfg.FundingTrap.PostSettleTimeout); d > configured {
		configured = d
	}
	if configured == timeout {
		return timeout
	}
	return configured + 5*time.Second
}

func (r *Runtime) publishPositionClosedFromUpdate(ctx context.Context, reqID string, pos exchange.PersonalPositionUpdate) {
	if pos.HoldVol > 0 {
		return
	}

	topic, flow, fill, ok := r.closedPositionFlow(pos)
	if !ok || !r.TryMarkFlowTerminal(flow) {
		return
	}
	if flow == events.FlowTrap {
		r.MarkTrapTerminal()
	}

	r.RecordAndPublishCtx(ctx, reqID, topic, events.PositionClosedEvent{
		Flow:       flow,
		Symbol:     pos.Symbol,
		ClosePrice: firstPositive(pos.CloseAvgPrice, pos.NewCloseAvgPrice),
		CloseVol:   firstPositive(pos.CloseVol, fill.FillVol),
		Reason:     "position_update_closed",
		Profit:     firstNonZero(pos.CloseProfitLoss, pos.Realized, pos.PNL),
		Fee:        pos.Fee,
		Method:     "ws_position",
	})
}

func (r *Runtime) closedPositionFlow(pos exchange.PersonalPositionUpdate) (string, string, events.OrderFilledEvent, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	reversionFill, hasReversion := r.activeReversionFillLocked()
	trapFill, hasTrap := r.activeTrapFillLocked()

	if hasReversion && positionTypeMatchesOpenSide(pos.PositionType, reversionFill.Side) {
		return events.TopicReversionPositionClosed, events.FlowReversion, reversionFill, true
	}
	if hasTrap && positionTypeMatchesOpenSide(pos.PositionType, trapFill.Side) {
		return events.TopicTrapPositionClosed, events.FlowTrap, trapFill, true
	}
	if pos.PositionType != 0 {
		return "", "", events.OrderFilledEvent{}, false
	}
	if hasReversion && !hasTrap {
		return events.TopicReversionPositionClosed, events.FlowReversion, reversionFill, true
	}
	if hasTrap && !hasReversion {
		return events.TopicTrapPositionClosed, events.FlowTrap, trapFill, true
	}
	return "", "", events.OrderFilledEvent{}, false
}

func (r *Runtime) activeReversionFillLocked() (events.OrderFilledEvent, bool) {
	if r.reversionFill == nil || r.terminal[events.FlowReversion] {
		return events.OrderFilledEvent{}, false
	}
	return *r.reversionFill, true
}

func (r *Runtime) activeTrapFillLocked() (events.OrderFilledEvent, bool) {
	if r.trapFill == nil || r.trapTerminal || r.terminal[events.FlowTrap] {
		return events.OrderFilledEvent{}, false
	}
	return *r.trapFill, true
}

func positionTypeMatchesOpenSide(positionType int, side shared.Side) bool {
	switch positionType {
	case 1:
		return side == shared.SideOpenLong
	case -1, 2, 3:
		return side == shared.SideOpenShort
	default:
		return false
	}
}

func firstPositive(values ...float64) float64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstNonZero(values ...float64) float64 {
	for _, value := range values {
		if value != 0 {
			return value
		}
	}
	return 0
}

// CalculateFinalPnL computes the total PnL from the event log at cycle end.
func (r *Runtime) CalculateFinalPnL() events.FinalPnLEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	result := events.FinalPnLEvent{
		Symbol: r.cfg.Symbol,
	}

	for i := range r.eventLog {
		switch r.eventLog[i].Topic {
		case events.TopicReversionOrderFilled:
			var fillEvt events.OrderFilledEvent
			if err := json.Unmarshal(r.eventLog[i].Payload, &fillEvt); err == nil {
				result.IocPnL += fillEvt.Profit
				result.TradingFees += fillEvt.Fee
			}
		case events.TopicTrapOrderFilled:
			var fillEvt events.OrderFilledEvent
			if err := json.Unmarshal(r.eventLog[i].Payload, &fillEvt); err == nil {
				result.TrapPnL += fillEvt.Profit
				result.TradingFees += fillEvt.Fee
			}
		}
	}

	result.TotalPnL = result.IocPnL + result.TrapPnL
	result.NetPnL = result.TotalPnL - result.TradingFees - result.FundingFeePaid

	return result
}

// PublishFinalPnL calculates and publishes the final PnL event.
func (r *Runtime) PublishFinalPnL(ctx context.Context, reqID string) {
	r.PublishFinalPnLCtx(ctx, reqID)
}

// PublishFinalPnLCtx calculates and publishes the final PnL event.
func (r *Runtime) PublishFinalPnLCtx(ctx context.Context, reqID string) {
	pnl := r.CalculateFinalPnL()
	pnl.Journey = r.JourneyEvents()
	pnl.EventCount = len(pnl.Journey)
	r.RecordAndPublishCtx(ctx, reqID, events.TopicCycleFinalPnL, pnl)
	r.logFinalPnL(ctx, pnl)
}

func (r *Runtime) logFinalPnL(ctx context.Context, pnl events.FinalPnLEvent) {
	raw, err := json.Marshal(pnl)
	if err != nil {
		applogger.WithCtx(ctx, r.deps.Log).Warn("Cycle final PnL journal marshal failed",
			slog.String("symbol", pnl.Symbol),
			slog.Any("error", err),
		)
		return
	}
	applogger.WithCtx(ctx, r.deps.Log).Info("Cycle final PnL journal",
		slog.String("symbol", pnl.Symbol),
		slog.Float64("net_pnl", pnl.NetPnL),
		slog.Int("event_count", pnl.EventCount),
		slog.String("payload", string(raw)),
	)
}

// trackStateToString converts track order state to string.
func trackStateToString(state int) string {
	switch state {
	case 0:
		return "active"
	case 1:
		return "triggered"
	case 2:
		return "cancelled"
	default:
		return "unknown"
	}
}

func (r *Runtime) TryMarkFlowTerminal(flow string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.terminal == nil {
		r.terminal = make(map[string]bool)
	}
	if r.terminal[flow] {
		return false
	}
	r.terminal[flow] = true
	return true
}

func (r *Runtime) IsFlowTerminal(flow string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.terminal != nil && r.terminal[flow]
}

func (r *Runtime) FinalizeExcursion(ctx context.Context, reqID string) bool {
	// Check if any order was filled by checking event log
	r.mu.Lock()
	var iocFilled, trapFilled bool
	for i := range r.eventLog {
		if r.eventLog[i].Topic == events.TopicReversionOrderFilled {
			iocFilled = true
		}
		if r.eventLog[i].Topic == events.TopicTrapOrderFilled {
			trapFilled = true
		}
	}
	r.mu.Unlock()

	if !iocFilled && !trapFilled {
		return false
	}
	if pd, err := r.deps.PriceStore.GetPrice(ctx, r.cfg.Symbol, 2*time.Second); err == nil {
		r.mu.Lock()
		r.eventSeq++
		env := events.JournalEnvelope{
			Seq:     r.eventSeq,
			Time:    time.Now(),
			ReqID:   reqID,
			Symbol:  r.cfg.Symbol,
			Flow:    events.FlowCycle,
			Topic:   events.TopicExcursionPriceObserved,
			Payload: []byte(fmt.Sprintf(`{"price":%f}`, pd.LastPrice)),
		}
		r.eventLog = append(r.eventLog, env)
		r.mu.Unlock()
		return true
	}
	return false
}

func (r *Runtime) CycleRiskAllowsReversion(c domain.Candidate) error {
	return nil
}

func (r *Runtime) CycleRiskAllowsTrap(c domain.Candidate, trapNotional float64) error {
	return nil
}

func CalcSpreadPct(bestBid, bestAsk float64) float64 {
	if bestBid <= 0 {
		return 0
	}
	return decmath.Mul(decmath.Div(decmath.Sub(bestAsk, bestBid), bestBid), 100.0)
}

func ToTradeConfig(sc config.SymbolConfig) domain.TradeConfig {
	return domain.TradeConfig{
		Symbol:              sc.Symbol,
		SimulateSettle:      sc.SimulateSettle,
		MaxPriceDiffPercent: sc.MaxPriceDiffPercent,
		MarginUSDT:          sc.MarginUSDT,
		Leverage:            sc.Leverage,
		FundingReversion:    sc.FundingReversion,
		FundingTrap:         sc.FundingTrap,
		ParsedOpenType:      sc.ParsedOpenType,
		ParsedPositionMode:  sc.ParsedPositionMode,
	}
}

func Unmarshal[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("unmarshal %T: %w", v, err)
	}
	return v, nil
}
