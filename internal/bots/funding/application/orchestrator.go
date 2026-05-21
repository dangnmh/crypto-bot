package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding/application/cycle"
	"crypto-bot/internal/bots/funding/application/reversion"
	"crypto-bot/internal/bots/funding/application/trap"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/observability"
)

type Deps = cycle.Deps

// CycleOrchestrator manages one funding cycle and wires the independent flows.
type CycleOrchestrator struct {
	rt *cycle.Runtime
}

func NewCycleOrchestrator(
	cfg config.SymbolConfig,
	global *config.Config,
	deps Deps,
) *CycleOrchestrator {
	return &CycleOrchestrator{
		rt: cycle.NewRuntime(cfg, global, deps),
	}
}

func (o *CycleOrchestrator) Run(ctx context.Context, settle time.Time) {
	ctx = observability.WithCorrelationID(ctx)
	cycleCtx, cancelCycle := context.WithCancel(ctx)
	defer cancelCycle()
	reqID := observability.CorrelationID(ctx)
	log := o.rt.Log().With("req_id", reqID)

	log.Info("━━━ Cycle start ━━━", slog.Time("settle", settle))
	o.rt.Begin(reqID, settle, log)
	defer func() { _ = o.rt.CloseBus() }()
	defer o.rt.DumpTimeline(log)

	done := make(chan struct{})
	o.setupEventChain(cycleCtx, done)

	if err := o.rt.PublishStart(settle); err != nil {
		log.Error("🔴 Failed to publish cycle start", slog.Any("error", err))
		return
	}

	select {
	case <-done:
		log.Info("━━━ Cycle end ━━━")
	case <-ctx.Done():
		log.Info("━━━ Cycle cancelled ━━━")
	}
	cancelCycle()
}

func (o *CycleOrchestrator) setupEventChain(ctx context.Context, done chan struct{}) {
	reqID := o.rt.GetReqID()
	o.subscribeScan(ctx)
	reversion.Register(ctx, o.rt)
	trap.Register(ctx, o.rt)
	o.rt.SubscribeWSOrderEvents(ctx, reqID, o.rt.Config().Symbol)
	o.subscribeCleanup(ctx, done)
	o.subscribeEventLog(ctx)
}

func (o *CycleOrchestrator) abort(source, reason string) {
	reqID := o.rt.GetReqID()
	o.rt.Abort(reqID, source, reason)
}

func unmarshal[T any](data []byte) (T, error) {
	return cycle.Unmarshal[T](data)
}
