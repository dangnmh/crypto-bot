package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding_reversion/config"
	"crypto-bot/internal/infrastructure/exchange"
	"crypto-bot/internal/infrastructure/store"
	"crypto-bot/internal/infrastructure/timesync"
	"crypto-bot/internal/infrastructure/watcher"
	"crypto-bot/internal/infrastructure/ws"
)

// ──────────────────────────────────────────────────────────────────────
// symbolWorker — isolated per-symbol execution unit
// ──────────────────────────────────────────────────────────────────────.

type symbolWorker struct {
	cfg           config.SymbolConfig
	global        *config.Config
	client        exchange.Client
	ws            ws.Subscriber
	orderNotifier watcher.OrderNotifier
	tickerStore   store.TickerReader
	contractStore store.ContractReader
	priceStore    store.PriceReader
	fundingStore  store.FundingReader
	klineStore    store.KlineReadWriter
	depthStore    store.DepthReader
	ts            *timesync.TimeSync
	log           *slog.Logger
	trailing      *TrailingManager
	subs          *SubscriptionManager
}

func (w *symbolWorker) run(ctx context.Context) {
	settle, err := w.nextSettleTime()
	if err != nil {
		w.log.Error("🔴 No settle time; retry in 1m", "error", err)
		if !w.sleep(ctx, time.Minute) {
			return
		}
		return
	}

	// Wait until T - 5 minutes before actively entering the cycle
	if d := w.ts.Until(settle.Add(-5 * time.Minute)); d > 0 {
		w.log.Debug("😴 Waiting for cycle window", "settle", settle, "wait", d)
		if !w.sleep(ctx, d) {
			return
		}
	}

	// If we are somehow already past the firing deadline (T - 5s), skip
	if w.ts.Until(settle.Add(-5*time.Second)) <= 0 {
		w.log.Warn("🔴 Settle time passed or missed", "settle", settle)
		return
	}

	// Execute one funding cycle via FSM
	w.cycle(ctx, settle)
}

// ──────────────────────────────────────────────────────────────────────
// FSM Cycle — dispatch loop
// ──────────────────────────────────────────────────────────────────────.

// cycle orchestrates one complete funding reversion cycle via FSM.
// All phase logic lives inside FSM callbacks (see state.go).
func (w *symbolWorker) cycle(ctx context.Context, settle time.Time) {
	w.log.Info("━━━ Cycle start ━━━", "settle", settle)

	cs := &CycleState{Settle: settle, NextEvent: EvStart}
	machine := NewCycleFSM(w.log, w, cs)

	for cs.NextEvent != "" && ctx.Err() == nil {
		if err := machine.Event(ctx, cs.NextEvent); err != nil {
			w.log.Error("🔴 FSM error", "error", err)
			break
		}
	}
}
