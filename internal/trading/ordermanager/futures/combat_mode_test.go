package futures_test

import (
	"log/slog"
	"runtime/debug"
	"sync/atomic"
	"testing"
	"time"

	"crypto-bot/internal/trading/ordermanager/futures"
)

//nolint:paralleltest // Mutates process-global GC settings, cannot run in parallel
func TestCombatModeCoordinator_ImmediateEntry(t *testing.T) {
	origGC := debug.SetGCPercent(100)
	defer debug.SetGCPercent(origGC)

	coord := futures.NewCombatModeCoordinator(slog.Default())
	defer coord.Close()

	coord.SetDurations(50*time.Millisecond, 50*time.Millisecond)

	var entered, exited atomic.Bool
	coord.SetHooks(
		func(t time.Time) { entered.Store(true) },
		func() { exited.Store(true) },
	)

	// Target time is now (within lead 50ms and trailing 50ms)
	now := time.Now()
	coord.RegisterTargetTime(now)

	if !coord.IsActive() {
		t.Fatalf("expected combat mode to be active immediately")
	}
	if !entered.Load() {
		t.Fatalf("expected entered hook to have been called")
	}

	// Wait for trailing duration (50ms) to elapse + small buffer
	time.Sleep(80 * time.Millisecond)

	if coord.IsActive() {
		t.Fatalf("expected combat mode to have exited")
	}
	if !exited.Load() {
		t.Fatalf("expected exited hook to have been called")
	}
}

//nolint:paralleltest // Mutates process-global GC settings, cannot run in parallel
func TestCombatModeCoordinator_FutureScheduled(t *testing.T) {
	origGC := debug.SetGCPercent(100)
	defer debug.SetGCPercent(origGC)

	coord := futures.NewCombatModeCoordinator(nil)
	defer coord.Close()

	coord.SetDurations(30*time.Millisecond, 30*time.Millisecond)

	var entered, exited atomic.Bool
	coord.SetHooks(
		func(t time.Time) { entered.Store(true) },
		func() { exited.Store(true) },
	)

	// Target is 60ms in future -> starts at 60ms - 30ms = 30ms, ends at 60ms + 30ms = 90ms
	target := time.Now().Add(60 * time.Millisecond)
	coord.RegisterTargetTime(target)

	if coord.IsActive() {
		t.Fatalf("expected combat mode to not be active immediately")
	}

	// At 45ms, should be active
	time.Sleep(45 * time.Millisecond)
	if !coord.IsActive() {
		t.Fatalf("expected combat mode to be active at 45ms")
	}
	if !entered.Load() {
		t.Fatalf("expected entered hook to be called")
	}

	// At 110ms, should have exited
	time.Sleep(65 * time.Millisecond)
	if coord.IsActive() {
		t.Fatalf("expected combat mode to have exited at 110ms")
	}
	if !exited.Load() {
		t.Fatalf("expected exited hook to be called")
	}
}

//nolint:paralleltest // Mutates process-global GC settings, cannot run in parallel
func TestCombatModeCoordinator_MultipleOverlappingOrders(t *testing.T) {
	origGC := debug.SetGCPercent(100)
	defer debug.SetGCPercent(origGC)

	coord := futures.NewCombatModeCoordinator(nil)
	defer coord.Close()

	coord.SetDurations(20*time.Millisecond, 40*time.Millisecond)

	var enterCount, exitCount atomic.Int32
	coord.SetHooks(
		func(t time.Time) { enterCount.Add(1) },
		func() { exitCount.Add(1) },
	)

	now := time.Now()
	// Order 1: target at +40ms -> window [+20ms, +80ms]
	// Order 2: target at +60ms -> window [+40ms, +100ms]
	coord.RegisterTargetTime(now.Add(40 * time.Millisecond))
	coord.RegisterTargetTime(now.Add(60 * time.Millisecond))

	// At 50ms, combat mode must be active
	time.Sleep(50 * time.Millisecond)
	if !coord.IsActive() {
		t.Fatalf("expected combat mode active at 50ms")
	}

	// At 85ms (after Order 1 trailing, but before Order 2 trailing at +100ms)
	// Combat mode MUST STILL BE ACTIVE!
	time.Sleep(35 * time.Millisecond)
	if !coord.IsActive() {
		t.Fatalf("expected combat mode STILL ACTIVE at 85ms due to overlapping Order 2")
	}
	if exitCount.Load() != 0 {
		t.Fatalf("expected 0 exits so far, got %d", exitCount.Load())
	}

	// Wait until +130ms (after Order 2 trailing)
	time.Sleep(45 * time.Millisecond)
	if coord.IsActive() {
		t.Fatalf("expected combat mode exited at 130ms")
	}
	if exitCount.Load() != 1 {
		t.Fatalf("expected exactly 1 exit, got %d", exitCount.Load())
	}
}

//nolint:paralleltest // Mutates process-global GC settings, cannot run in parallel
func TestCombatModeCoordinator_PastWindowIgnored(t *testing.T) {
	coord := futures.NewCombatModeCoordinator(nil)
	defer coord.Close()

	// Target 1 hour in the past
	coord.RegisterTargetTime(time.Now().Add(-1 * time.Hour))
	if coord.IsActive() {
		t.Fatalf("past target should not activate combat mode")
	}
}

//nolint:paralleltest // Mutates process-global GC settings, cannot run in parallel
func TestCombatModeCoordinator_Close(t *testing.T) {
	origGC := debug.SetGCPercent(100)
	defer debug.SetGCPercent(origGC)

	coord := futures.NewCombatModeCoordinator(nil)
	coord.SetDurations(50*time.Millisecond, 50*time.Millisecond)

	coord.RegisterTargetTime(time.Now())
	if !coord.IsActive() {
		t.Fatalf("expected active")
	}

	coord.Close()
	if coord.IsActive() {
		t.Fatalf("expected inactive after Close")
	}

	// Re-calling Close is idempotent
	coord.Close()
}
