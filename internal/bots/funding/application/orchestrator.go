package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding/application/events"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"
)

// ──────────────────────────────────────────────────────────────────────.
// CycleOrchestrator — event-driven cycle manager.
// ──────────────────────────────────────────────────────────────────────.

// Deps holds all external dependencies for CycleOrchestrator.
// Grouping them into a struct keeps the constructor readable and
// makes it straightforward to build from external test packages.
type Deps struct {
	Client        exchange.Client
	WsSub         ws.Subscriber
	OrderNotifier watcher.OrderNotifier
	TickerStore   store.TickerReader
	ContractStore store.ContractReader
	PriceStore    store.PriceReader
	FundingStore  store.FundingReader
	KlineStore    store.KlineReadWriter
	DepthStore    store.DepthReader
	Clock         shared.Clock
	Log           *slog.Logger
	CycleRecorder domain.CycleRecorder
}

// CycleOrchestrator manages one funding reversion cycle using Watermill
// event-driven architecture. Each phase is a handler that subscribes to
// an upstream event and publishes a downstream event, forming a chain.
type CycleOrchestrator struct {
	cfg    config.SymbolConfig
	global *config.Config
	deps   Deps
	subs   *SubscriptionManager

	// Per-cycle state.
	bus       *eventbus.Bus
	mu        sync.Mutex
	candidate domain.Candidate
	results   []OrderResult
	recorder  *domain.CycleRecordBuilder

	excursionCancel context.CancelFunc
}

// withLock executes the given closure while holding the orchestrator's mutex.
// This is the preferred pattern for reading or modifying cycle state safely,
// as it guarantees unlocking even if panics occur and eliminates manual unlock errors.
func (o *CycleOrchestrator) withLock(fn func()) {
	o.mu.Lock()
	defer o.mu.Unlock()
	fn()
}

// NewCycleOrchestrator creates a new orchestrator for a single cycle.
func NewCycleOrchestrator(
	cfg config.SymbolConfig,
	global *config.Config,
	deps Deps,
) *CycleOrchestrator {
	return &CycleOrchestrator{
		cfg:    cfg,
		global: global,
		deps:   deps,
		subs: NewSubscriptionManager(
			deps.WsSub,
			cfg.Symbol,
			toTradeConfig(cfg).FundingReversion.DynamicPricing,
			toTradeConfig(cfg).FundingReversion.ImbalanceFilter,
			deps.Log,
		),
	}
}

// Run executes the full cycle event chain. Blocks until the cycle completes,
// is aborted, times out, or context is cancelled.
func (o *CycleOrchestrator) Run(ctx context.Context, settle time.Time) {
	ctx = observability.WithCorrelationID(ctx)
	reqID := observability.CorrelationID(ctx)
	log := o.deps.Log.With("req_id", reqID)
	o.deps.Log = log

	log.Info("━━━ Cycle start ━━━", slog.Time("settle", settle))

	// Initialize cycle record builder.
	o.recorder = domain.NewCycleRecordBuilder(reqID, settle)

	// Create per-cycle event bus.
	o.bus = eventbus.New(log)
	defer func() { _ = o.bus.Close() }()
	defer o.bus.DumpTimeline(log)

	// Persist cycle record at end-of-cycle (before bus closes).
	defer o.persistCycleRecord(ctx)

	// Set up done signal.
	done := make(chan struct{})

	// Subscribe to all events BEFORE publishing (GoChannel requirement).
	o.setupEventChain(ctx, settle, done)

	// Kick off the chain.
	if err := o.bus.Publish(events.TopicScanStart, events.CycleStartEvent{
		Symbol:     o.cfg.Symbol,
		SettleTime: settle,
	}); err != nil {
		log.Error("🔴 Failed to publish cycle start", slog.Any("error", err))
		return
	}

	// Block until done or context cancelled.
	select {
	case <-done:
		log.Info("━━━ Cycle end ━━━")
	case <-ctx.Done():
		log.Info("━━━ Cycle cancelled ━━━")
	}
}

// setupEventChain subscribes handlers to form the event chain.
func (o *CycleOrchestrator) setupEventChain(ctx context.Context, settle time.Time, done chan struct{}) {
	// Shared scan.
	o.subscribeScan(ctx)

	// Reversion flow.
	o.subscribeArm(ctx)
	o.subscribeWait(ctx, settle)
	o.subscribeRecheck(ctx)
	o.subscribeFireIOC(ctx, settle)
	o.subscribeTimeoutGuard(ctx)

	// Trap flow.
	o.subscribeFireTrap(ctx, settle)
	o.subscribeTrapOrderTimeoutGuard(ctx)

	// Shared flow observers with flow-scoped topics.
	o.subscribeFillWatcher(ctx)
	o.subscribeTrailing(ctx)

	// Terminal handlers.
	o.subscribeCleanup(ctx, done)

	// Event logger (subscribes to all known topics).
	o.subscribeEventLog(ctx)
}

// ──────────────────────────────────────────────────────────────────────.
// Publishing helpers.
// ──────────────────────────────────────────────────────────────────────.

func (o *CycleOrchestrator) publishOrLog(topic string, payload any) {
	if err := o.bus.Publish(topic, payload); err != nil {
		o.deps.Log.Error("🔴 Publish failed", slog.String("topic", topic), slog.Any("error", err))
	}
}

func (o *CycleOrchestrator) abort(phase domain.Phase, reason string) {
	if phase == domain.PhaseScan {
		o.publishOrLog(events.TopicScanAbort, events.CycleAbortEvent{
			Symbol: o.cfg.Symbol,
			Reason: reason,
			Phase:  phase,
		})
	}
	o.publishOrLog(events.TopicReversionAbort, events.CycleAbortEvent{
		Flow:   events.FlowReversion,
		Symbol: o.cfg.Symbol,
		Reason: reason,
		Phase:  phase,
	})
}

// unmarshal is a helper to deserialize event payloads from messages.
func unmarshal[T any](data []byte) (T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return v, fmt.Errorf("unmarshal %T: %w", v, err)
	}
	return v, nil
}
