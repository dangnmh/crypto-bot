package futures

import (
	"log/slog"
	"runtime"
	"runtime/debug"
	"sync"
	"time"
)

const (
	defaultCombatLeadDuration     = 15 * time.Second
	defaultCombatTrailingDuration = 5 * time.Second
)

// CombatModeCoordinator coordinates system-wide high-priority execution windows around settle/fire times.
// Within the combat window (target - leadDuration to target + trailingDuration):
// - Pre-emptive runtime.GC() is called prior to the critical execution window.
// - Go Garbage Collector is disabled (SetGCPercent(-1)) to eliminate stop-the-world pauses during sleep and firing.
// - Concurrency-safe: multiple overlapping orders extend the combat window interval, preventing premature GC restoration.
// - Upon window exit, normal GC is restored and memory accumulated during the window is reclaimed.
type CombatModeCoordinator struct {
	mu                sync.Mutex
	log               *slog.Logger
	leadDuration      time.Duration
	trailingDuration  time.Duration
	isActive          bool
	activeUntil       time.Time
	originalGCPercent int
	timers            []*time.Timer
	isClosed          bool
	lastManualGCTime  time.Time
	onEnter           func(time.Time)
	onExit            func()
}

// NewCombatModeCoordinator creates a new CombatModeCoordinator.
func NewCombatModeCoordinator(log *slog.Logger) *CombatModeCoordinator {
	if log == nil {
		log = slog.Default()
	}
	return &CombatModeCoordinator{
		log:               log.With("component", "CombatModeCoordinator"),
		leadDuration:      defaultCombatLeadDuration,
		trailingDuration:  defaultCombatTrailingDuration,
		originalGCPercent: 100,
	}
}

// SetDurations configures custom lead and trailing durations (primarily for testing).
func (c *CombatModeCoordinator) SetDurations(lead, trailing time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.leadDuration = lead
	c.trailingDuration = trailing
}

// SetHooks sets callbacks for combat mode enter and exit events (useful for testing or metrics).
func (c *CombatModeCoordinator) SetHooks(onEnter func(time.Time), onExit func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.onEnter = onEnter
	c.onExit = onExit
}

// IsActive returns whether combat mode is currently active.
func (c *CombatModeCoordinator) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.isActive && time.Now().Before(c.activeUntil)
}

// RegisterTargetTime schedules or enters combat mode for a target settle or fire time.
func (c *CombatModeCoordinator) RegisterTargetTime(targetTime time.Time) {
	if targetTime.IsZero() {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return
	}

	now := time.Now()
	windowStart := targetTime.Add(-c.leadDuration)
	windowEnd := targetTime.Add(c.trailingDuration)

	// If window already expired, ignore
	if !now.Before(windowEnd) {
		return
	}

	// Case 1: Already inside the window [windowStart, windowEnd]
	if !now.Before(windowStart) {
		c.enterLocked(targetTime, windowEnd)
		return
	}

	// Case 2: Window starts in the future
	durUntilStart := windowStart.Sub(now)
	startTimer := time.AfterFunc(durUntilStart, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.isClosed {
			return
		}
		c.enterLocked(targetTime, windowEnd)
	})
	c.timers = append(c.timers, startTimer)

	c.log.Info("Scheduled combat window",
		slog.String("exchange", "system"),
		slog.Time("target_time", targetTime),
		slog.Time("window_start", windowStart),
		slog.Time("window_end", windowEnd),
		slog.Duration("lead", c.leadDuration),
		slog.Duration("trailing", c.trailingDuration),
	)
}

func (c *CombatModeCoordinator) enterLocked(targetTime, windowEnd time.Time) {
	if c.isClosed {
		return
	}

	// Extend activeUntil if windowEnd is further in the future
	if windowEnd.After(c.activeUntil) {
		c.activeUntil = windowEnd
	}

	if !c.isActive {
		c.isActive = true
		c.log.Info("⚔️ Entering COMBAT MODE: suppressing GC and entering high-priority execution window",
			slog.String("exchange", "system"),
			slog.Time("target_time", targetTime),
			slog.Time("active_until", c.activeUntil),
		)

		// Run pre-emptive GC before entering window, throttled to prevent spamming
		if time.Since(c.lastManualGCTime) > 2*time.Second {
			runtime.GC()
			c.lastManualGCTime = time.Now()
		}

		prev := debug.SetGCPercent(-1)
		if prev != -1 {
			c.originalGCPercent = prev
		}

		if c.onEnter != nil {
			c.onEnter(targetTime)
		}

		c.scheduleExitCheckLocked()
	}
}

func (c *CombatModeCoordinator) scheduleExitCheckLocked() {
	if c.isClosed || !c.isActive {
		return
	}

	now := time.Now()
	dur := c.activeUntil.Sub(now)
	if dur <= 0 {
		c.exitLocked()
		return
	}

	exitTimer := time.AfterFunc(dur, func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		if c.isClosed || !c.isActive {
			return
		}

		now := time.Now()
		// If activeUntil was extended while waiting, re-arm exit timer
		if now.Before(c.activeUntil) {
			c.scheduleExitCheckLocked()
			return
		}

		c.exitLocked()
	})
	c.timers = append(c.timers, exitTimer)
}

func (c *CombatModeCoordinator) exitLocked() {
	if !c.isActive || c.isClosed {
		return
	}

	c.isActive = false
	c.log.Info("🛡️ Exiting COMBAT MODE: restoring GC and reclaiming window memory",
		slog.String("exchange", "system"),
		slog.Int("restored_gc_percent", c.originalGCPercent),
	)

	debug.SetGCPercent(c.originalGCPercent)
	runtime.GC()
	c.lastManualGCTime = time.Now()

	if c.onExit != nil {
		c.onExit()
	}
}

// Close gracefully terminates all scheduled timers and restores GC if active.
func (c *CombatModeCoordinator) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.isClosed {
		return
	}
	c.isClosed = true

	for _, t := range c.timers {
		t.Stop()
	}
	c.timers = nil

	if c.isActive {
		c.isActive = false
		debug.SetGCPercent(c.originalGCPercent)
		runtime.GC()
	}
}
