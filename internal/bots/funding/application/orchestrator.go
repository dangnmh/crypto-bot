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
	applogger "crypto-bot/pkg/logger"
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
	if observability.ReqID(ctx) == "" {
		ctx = observability.WithCorrelationID(ctx)
	}
	ctx = observability.WithCycleID(ctx)
	cycleCtx, cancelCycle := context.WithCancel(ctx)
	defer cancelCycle()
	reqID := observability.ReqID(ctx)
	log := applogger.WithCtx(ctx, o.rt.Log())

	log.Info("━━━ Cycle start ━━━", slog.Time("settle", settle))
	o.rt.BeginWithContext(ctx, reqID, settle, o.rt.Log())
	defer func() { _ = o.rt.CloseBus() }()
	defer o.rt.DumpTimeline(o.rt.Log())

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

func (o *CycleOrchestrator) abort(ctx context.Context, source, reason string) {
	reqID := o.rt.GetReqID()
	o.rt.AbortCtx(ctx, reqID, source, reason)
}

func unmarshal[T any](data []byte) (T, error) {
	return cycle.Unmarshal[T](data)
}
