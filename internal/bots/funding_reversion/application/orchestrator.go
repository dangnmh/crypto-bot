package application

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	shared "crypto-bot/internal/domain"

	"crypto-bot/internal/bots/funding_reversion/application/events"
	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/bots/funding_reversion/domain"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/observability"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
	"crypto-bot/pkg/eventbus"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
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
	candidate domain.Candidate
	results   []OrderResult
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
		subs:   NewSubscriptionManager(deps.WsSub, cfg.Symbol, toTradeConfig(cfg).FundingReversion.DynamicPricing, deps.Log),
	}
}

// Run executes the full cycle event chain. Blocks until the cycle completes,
// is aborted, times out, or context is cancelled.
func (o *CycleOrchestrator) Run(ctx context.Context, settle time.Time) {
	// OTel trace for cycle.
	tracer := otel.Tracer("funding-reversion")
	ctx, span := tracer.Start(ctx, fmt.Sprintf("cycle/%s", o.cfg.Symbol),
		trace.WithAttributes(
			attribute.String("symbol", o.cfg.Symbol),
			attribute.String("settle", settle.Format(time.RFC3339)),
		),
	)
	defer span.End()

	ctx = observability.WithCorrelationID(ctx)
	reqID := observability.CorrelationID(ctx)
	log := o.deps.Log.With("req_id", reqID)
	o.deps.Log = log

	log.Info("━━━ Cycle start ━━━", "settle", settle)

	// Create per-cycle event bus.
	o.bus = eventbus.New(log)
	defer func() { _ = o.bus.Close() }()
	defer o.bus.DumpTimeline(log)

	// Set up done signal.
	done := make(chan struct{})

	// Subscribe to all events BEFORE publishing (GoChannel requirement).
	o.setupEventChain(ctx, settle, done)

	// Kick off the chain.
	if err := o.bus.Publish(events.TopicCycleStart, events.CycleStartEvent{
		Symbol:     o.cfg.Symbol,
		SettleTime: settle,
	}); err != nil {
		log.Error("🔴 Failed to publish cycle start", "error", err)
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
	// Pre-settle chain (sequential).
	o.subscribeScan(ctx)
	o.subscribeArm(ctx)
	o.subscribeWait(ctx, settle)
	o.subscribeRecheck(ctx)
	o.subscribeFireIOC(ctx, settle)

	// Post-settle handlers (concurrent).
	o.subscribeFillWatcher(ctx)
	o.subscribeTrailing(ctx)
	o.subscribeTimeoutGuard(ctx)
	o.subscribeFireTrap(ctx, settle)

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
		o.deps.Log.Error("🔴 Publish failed", "topic", topic, "error", err)
	}
}

func (o *CycleOrchestrator) abort(phase, reason string) {
	o.publishOrLog(events.TopicCycleAbort, events.CycleAbortEvent{
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
