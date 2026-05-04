package application

import (
	"context"
	"log/slog"
	"time"

	"crypto-bot/internal/bots/funding_reversion/domain"

	"github.com/looplab/fsm"
)

// ──────────────────────────────────────────────────────────────────────
// States
// ──────────────────────────────────────────────────────────────────────.

const (
	StateIdle     = "idle"
	StateScan     = "scan"
	StateArm      = "arm"
	StateWait     = "wait"
	StateRecheck  = "recheck"
	StateFireIOC  = "fire_ioc"  // Sniper IOC at T - offset
	StateFireTrap = "fire_trap" // Limit Trap at T + offset
	StateDone     = "done"
	StateAbort    = "abort"
)

// ──────────────────────────────────────────────────────────────────────
// Events
// ──────────────────────────────────────────────────────────────────────.

const (
	EvStart    = "start"
	EvArm      = "ev_arm"
	EvWait     = "ev_wait"
	EvRecheck  = "ev_recheck"
	EvFireIOC  = "ev_fire_ioc"
	EvFireTrap = "ev_fire_trap"
	EvDone     = "ev_done"
	EvAbort    = "ev_abort"
)

// ──────────────────────────────────────────────────────────────────────
// CycleState
// ──────────────────────────────────────────────────────────────────────.

type CycleState struct {
	Settle    time.Time
	Candidate domain.Candidate
	Results   []OrderResult
	NextEvent string
}

// ──────────────────────────────────────────────────────────────────────
// FSM Factory
// ──────────────────────────────────────────────────────────────────────.

func NewCycleFSM(logger *slog.Logger, w *symbolWorker, cs *CycleState) *fsm.FSM {
	return fsm.NewFSM(
		StateIdle,
		fsm.Events{
			{Name: EvStart, Src: []string{StateIdle}, Dst: StateScan},
			{Name: EvArm, Src: []string{StateScan}, Dst: StateArm},
			{Name: EvWait, Src: []string{StateArm}, Dst: StateWait},
			{Name: EvRecheck, Src: []string{StateWait}, Dst: StateRecheck},
			{Name: EvFireIOC, Src: []string{StateRecheck}, Dst: StateFireIOC},
			{Name: EvFireTrap, Src: []string{StateFireIOC}, Dst: StateFireTrap},
			{Name: EvDone, Src: []string{StateFireIOC, StateFireTrap}, Dst: StateDone},
			{Name: EvAbort, Src: []string{
				StateScan, StateArm, StateWait, StateRecheck,
				StateFireIOC, StateFireTrap,
			}, Dst: StateAbort},
		},
		fsm.Callbacks{
			"enter_state": func(_ context.Context, e *fsm.Event) {
				if e.Dst != StateIdle {
					logger.Info("⚙️ "+e.Dst, "from", e.Src)
				}
			},

			"enter_scan": func(_ context.Context, e *fsm.Event) {
				if w.onScan(cs) {
					cs.NextEvent = EvArm
				} else {
					cs.NextEvent = EvAbort
				}
			},

			"enter_arm": func(ctx context.Context, e *fsm.Event) {
				if w.onArm(ctx, cs) {
					cs.NextEvent = EvWait
				} else {
					cs.NextEvent = EvAbort
				}
			},

			"enter_wait": func(ctx context.Context, e *fsm.Event) {
				w.onWait(ctx, cs)
				cs.NextEvent = EvRecheck
			},

			"enter_recheck": func(_ context.Context, e *fsm.Event) {
				if w.onRecheck(cs) {
					cs.NextEvent = EvFireIOC
				} else {
					cs.NextEvent = EvAbort
				}
			},

			"enter_fire_ioc": func(ctx context.Context, e *fsm.Event) {
				w.onFireIOC(ctx, cs)
				// If hedge trap enabled → fire_trap, otherwise → done
				if cs.Candidate.Config.IsHedgeTrapEnabled() {
					cs.NextEvent = EvFireTrap
				} else {
					cs.NextEvent = EvDone
				}
			},

			"enter_fire_trap": func(ctx context.Context, e *fsm.Event) {
				w.onFireTrap(ctx, cs)
				cs.NextEvent = EvDone
			},

			"enter_done": func(_ context.Context, e *fsm.Event) {
				logger.Info("✅ Cycle complete")
				w.subs.UnsubscribeAll()
				cs.NextEvent = ""
			},

			"enter_abort": func(_ context.Context, e *fsm.Event) {
				logger.Info("⛔ Cycle aborted")
				w.subs.UnsubscribeAll()
				cs.NextEvent = ""
			},
		},
	)
}
