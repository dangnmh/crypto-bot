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
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/decmath"
	"crypto-bot/pkg/eventbus"

	"github.com/ThreeDotsLabs/watermill/message"
)

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

func (r *Runtime) Begin(reqID string, settle time.Time, log *slog.Logger) {
	r.deps.Log = log
	r.bus = eventbus.New(log)
	r.terminal = make(map[string]bool)
	r.reqID = reqID
	r.settle = settle
	r.eventSeq = 0
	r.eventLog = make([]events.JournalEnvelope, 0, 32)
	cfgJSON, _ := json.Marshal(ToTradeConfig(r.cfg))
	r.RecordAndPublish(reqID, events.TopicCycleStarted, events.CycleStartEvent{
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
func (r *Runtime) RecordAndPublish(reqID, topic string, payload any) {
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
		r.deps.Log.Error("Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}
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
		r.deps.Log.Error("Failed to subscribe to topic", slog.String("topic", topic), slog.Any("error", err))
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

func (r *Runtime) Publish(topic string, payload any) {
	if err := r.bus.Publish(topic, payload); err != nil {
		r.deps.Log.Error("🔴 Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}
}

func (r *Runtime) PublishStart(settle time.Time) error {
	return r.bus.Publish(events.TopicScanStart, events.CycleStartEvent{
		Symbol:     r.cfg.Symbol,
		SettleTime: settle,
	})
}

func (r *Runtime) Abort(reqID, source, reason string) {
	if source == "scan" {
		r.RecordAndPublish(reqID, events.TopicScanCandidateFound, events.CandidateFoundEvent{
			Flow:   events.FlowScan,
			Symbol: r.cfg.Symbol,
		}) // placeholder for scan rejected
		r.Publish(events.TopicScanAbort, events.CycleAbortEvent{
			Symbol: r.cfg.Symbol,
			Reason: reason,
		})
	}
	if !r.TryMarkFlowTerminal(events.FlowReversion) {
		return
	}
	r.RecordAndPublish(reqID, events.TopicReversionAbort, events.CycleAbortEvent{
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
		r.deps.Log.Warn("🟡 No contract data — skip")
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
		r.deps.Log.Debug("⏱️ wait", slog.Time("target", target), slog.Duration("wait", d))
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
	// Subscribe to position updates
	r.deps.OrderNotifier.OnPositionUpdate(ctx, symbol, 30*time.Second, func(pos exchange.PersonalPositionUpdate) {
		r.RecordAndPublish(reqID, events.TopicPositionUpdated, events.WSPositionUpdatedEvent{
			Symbol:        pos.Symbol,
			Side:          pos.PositionType,
			Size:          pos.HoldVol,
			EntryPrice:    pos.HoldAvgPrice,
			MarkPrice:     pos.LiquidatePrice,
			UnrealizedPnL: pos.Realized,
			Leverage:      pos.Leverage,
			Timestamp:     time.Now(),
		})
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
		r.RecordAndPublish(reqID, events.TopicTrackUpdated, events.WSTrackUpdatedEvent{
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
func (r *Runtime) PublishFinalPnL(reqID string) {
	pnl := r.CalculateFinalPnL()
	pnl.Journey = r.JourneyEvents()
	pnl.EventCount = len(pnl.Journey)
	r.RecordAndPublish(reqID, events.TopicCycleFinalPnL, pnl)
	r.logFinalPnL(pnl)
}

func (r *Runtime) logFinalPnL(pnl events.FinalPnLEvent) {
	raw, err := json.Marshal(pnl)
	if err != nil {
		r.deps.Log.Warn("Cycle final PnL journal marshal failed",
			slog.String("symbol", pnl.Symbol),
			slog.Any("error", err),
		)
		return
	}
	r.deps.Log.Info("Cycle final PnL journal",
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
	return r.checkCycleRisk(c.ReversionNotionalUSDT(), 0, false)
}

func (r *Runtime) CycleRiskAllowsTrap(c domain.Candidate, trapNotional float64) error {
	return r.checkCycleRisk(c.ReversionNotionalUSDT(), trapNotional, true)
}

func (r *Runtime) checkCycleRisk(reversionNotional, trapNotional float64, includeTrap bool) error {
	safety := r.global.System.Safety

	cycleNotional := reversionNotional
	if includeTrap {
		cycleNotional = decmath.Add(cycleNotional, trapNotional)
	}
	if safety.MaxCycleNotionalUSDT > 0 && cycleNotional > safety.MaxCycleNotionalUSDT {
		return fmt.Errorf("cycle notional %.4f exceeds max %.4f", cycleNotional, safety.MaxCycleNotionalUSDT)
	}

	reversionLoss := decmath.Mul(reversionNotional, r.cfg.FundingReversion.StopLossPct)
	cycleLoss := reversionLoss
	if includeTrap {
		cycleLoss = decmath.Add(cycleLoss, decmath.Mul(trapNotional, r.cfg.FundingTrap.StopLossPct))
	}
	if safety.MaxCycleLossUSDT > 0 && cycleLoss > safety.MaxCycleLossUSDT {
		return fmt.Errorf("cycle loss %.4f exceeds max %.4f", cycleLoss, safety.MaxCycleLossUSDT)
	}

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
