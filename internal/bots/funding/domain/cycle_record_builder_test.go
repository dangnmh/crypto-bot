package domain_test

import (
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
)

func TestCycleRecordBuilder_BuildAbortedCycle(t *testing.T) {
	t.Parallel()

	settle := time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC)
	b := domain.NewCycleRecordBuilder("req-1", settle)

	b.FRAtScan = 0.007
	b.Side = shared.SideOpenLong
	b.AbortReason = "FR below threshold"
	b.AbortPhase = domain.PhaseRecheck

	record := b.Build("BTC_USDT", 0, 0, nil, nil)

	assert.Equal(t, "req-1", record.ReqID)
	assert.Equal(t, "BTC_USDT", record.Symbol)
	assert.Equal(t, domain.OutcomeAborted, record.Outcome)
	assert.Equal(t, "FR below threshold", record.AbortReason)
	assert.Equal(t, domain.PhaseRecheck, record.AbortPhase)
	assert.InDelta(t, 0.007, record.Decision.FRAtScan, 1e-9)
	assert.Equal(t, shared.SideOpenLong, record.Decision.Side)
}

func TestCycleRecordBuilder_BuildFilledCycle(t *testing.T) {
	t.Parallel()

	settle := time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC)
	b := domain.NewCycleRecordBuilder("req-2", settle)

	// Decision phase.
	b.FRAtScan = 0.007
	b.FRAtRecheck = 0.0068
	b.Side = shared.SideOpenLong
	b.SafetyPassed = true

	// IOC execution.
	b.FireTimestamp = settle.Add(-42 * time.Millisecond)
	b.IOCIntended = 0.2449
	b.IOCFillPrice = 0.2450
	b.IOCFillVol = 100
	b.IOCFilled = true
	b.IOCOrderID = "order-123"
	b.LatencyRTTMs = 28

	// Exit.
	b.ExitReason = "trailing"
	b.ExitTime = settle.Add(25 * time.Second)

	// Trailing.
	b.TrailingActivated = true
	b.TrailingActivePrice = 0.2480
	b.TrailingCallbackPct = 0.5

	record := b.Build("STEEM_USDT", 0.03, 0.03, nil, nil)

	assert.Equal(t, domain.OutcomeProfit, record.Outcome)
	assert.Equal(t, "STEEM_USDT", record.Symbol)
	assert.True(t, record.Decision.FRChanged)
	assert.True(t, record.IOC.Filled)
	assert.InDelta(t, 0.2450, record.IOC.FillPrice, 1e-9)
	assert.Equal(t, int64(-42), record.IOC.SettleOffsetMs)
	assert.Equal(t, int64(28), record.IOC.LatencyRTTMs)
	assert.Equal(t, "trailing", record.Exit.Reason)
	assert.True(t, record.Exit.TrailingActivated)
	assert.Greater(t, record.Exit.HoldDurationMs, int64(0))
}

func TestCycleRecordBuilder_BuildTimeoutCycle(t *testing.T) {
	t.Parallel()

	b := domain.NewCycleRecordBuilder("req-3", time.Now())
	b.IOCFilled = true
	b.ExitReason = "timeout"

	record := b.Build("ETH_USDT", 0, 0, nil, nil)
	assert.Equal(t, domain.OutcomeTimeout, record.Outcome)
}

func TestCycleRecordBuilder_BuildNoFillCycle(t *testing.T) {
	t.Parallel()

	b := domain.NewCycleRecordBuilder("req-4", time.Now())
	b.IOCFilled = false
	b.ExitReason = "position_closed"

	record := b.Build("ETH_USDT", 0, 0, nil, nil)
	assert.Equal(t, domain.OutcomeNoFill, record.Outcome)
}

func TestCycleRecordBuilder_BuildStopLossCycle(t *testing.T) {
	t.Parallel()

	b := domain.NewCycleRecordBuilder("req-5", time.Now())
	b.IOCFilled = true
	b.ExitReason = "sl"

	record := b.Build("ETH_USDT", 0, 0, nil, nil)
	assert.Equal(t, domain.OutcomeLoss, record.Outcome)
}

func TestCycleRecordBuilder_TrapData(t *testing.T) {
	t.Parallel()

	b := domain.NewCycleRecordBuilder("req-7", time.Now())
	b.TrapEnabled = true
	b.TrapSource = "ob_monitor"
	b.TrapPrice = 0.2420
	b.TrapFilled = true
	b.TrapFillPrice = 0.2421
	b.TrapOrderID = "trap-order-1"
	b.IOCFilled = true
	b.ExitReason = "tp"

	record := b.Build("ETH_USDT", 0, 0, nil, nil)

	assert.True(t, record.Trap.Enabled)
	assert.Equal(t, "ob_monitor", record.Trap.Source)
	assert.InDelta(t, 0.2420, record.Trap.Price, 1e-9)
	assert.True(t, record.Trap.Filled)
	assert.InDelta(t, 0.2421, record.Trap.FillPrice, 1e-9)
	assert.Equal(t, "trap-order-1", record.Trap.OrderID)
}

//nolint:dupl // similar test setup to TestExcursionTracker_Short
func TestExcursionTracker_Long(t *testing.T) {
	t.Parallel()

	tracker := domain.NewExcursionTracker(shared.SideOpenLong, 100.0)

	// Price goes up (favorable for LONG).
	tracker.Update(102.0, time.Now())
	tracker.Update(105.0, time.Now()) // MFE
	tracker.Update(103.0, time.Now())
	tracker.Update(98.0, time.Now()) // MAE
	tracker.Update(101.0, time.Now())

	snap := tracker.Snapshot(shared.SideOpenLong, 100.0, 0.03, 0.03)

	assert.InDelta(t, 105.0, snap.MFEPrice, 1e-9)
	assert.InDelta(t, 5.0, snap.MFEPct, 1e-6)
	assert.InDelta(t, 98.0, snap.MAEPrice, 1e-9)
	assert.InDelta(t, 2.0, snap.MAEPct, 1e-6)
}

//nolint:dupl // similar test setup to TestExcursionTracker_Long
func TestExcursionTracker_Short(t *testing.T) {
	t.Parallel()

	tracker := domain.NewExcursionTracker(shared.SideOpenShort, 100.0)

	// Price goes down (favorable for SHORT).
	tracker.Update(98.0, time.Now())
	tracker.Update(95.0, time.Now()) // MFE
	tracker.Update(97.0, time.Now())
	tracker.Update(103.0, time.Now()) // MAE
	tracker.Update(99.0, time.Now())

	snap := tracker.Snapshot(shared.SideOpenShort, 100.0, 0.03, 0.03)

	assert.InDelta(t, 95.0, snap.MFEPrice, 1e-9)
	assert.InDelta(t, 5.0, snap.MFEPct, 1e-6)
	assert.InDelta(t, 103.0, snap.MAEPrice, 1e-9)
	assert.InDelta(t, 3.0, snap.MAEPct, 1e-6)
}

func TestExcursionTracker_HindsightValues(t *testing.T) {
	t.Parallel()

	tracker := domain.NewExcursionTracker(shared.SideOpenLong, 100.0)
	tracker.Update(105.0, time.Now()) // MFE = 5%
	tracker.Update(98.0, time.Now())  // MAE = 2%

	// TP configured at 3% (stored as ratio 0.03).
	snap := tracker.Snapshot(shared.SideOpenLong, 100.0, 0.03, 0.03)

	// MFE vs TP: 5% - 3% = 2% (TP was conservative).
	assert.InDelta(t, 2.0, snap.MFEvsTP, 1e-6)
	// MAE vs SL: 2% - 3% = -1% (SL was safe).
	assert.InDelta(t, -1.0, snap.MAEvsSL, 1e-6)
	// Ideal TP: 5% * 0.8 = 4%.
	assert.InDelta(t, 4.0, snap.IdealTPPct, 1e-6)
	// Ideal SL: 2% * 1.2 = 2.4%.
	assert.InDelta(t, 2.4, snap.IdealSLPct, 1e-6)
	// SL was not touched (2% < 3%).
	assert.False(t, snap.SLWasTouched)
	// TP efficiency: 3/5 = 0.6.
	assert.InDelta(t, 0.6, snap.TPEfficiency, 1e-6)
}

func TestExcursionTracker_IgnoresInvalidPrices(t *testing.T) {
	t.Parallel()

	tracker := domain.NewExcursionTracker(shared.SideOpenLong, 100.0)
	tracker.Update(0, time.Now())
	tracker.Update(-5, time.Now())

	// MFE/MAE should still be at entry price.
	snap := tracker.Snapshot(shared.SideOpenLong, 100.0, 0.03, 0.03)
	assert.InDelta(t, 100.0, snap.MFEPrice, 1e-9)
	assert.InDelta(t, 100.0, snap.MAEPrice, 1e-9)
}

func TestExcursionTracker_SnapshotZeroEntryPrice(t *testing.T) {
	t.Parallel()

	tracker := domain.NewExcursionTracker(shared.SideOpenLong, 0)
	snap := tracker.Snapshot(shared.SideOpenLong, 0, 0.03, 0.03)

	assert.InDelta(t, 0.0, snap.MFEPct, 1e-9)
	assert.InDelta(t, 0.0, snap.MAEPct, 1e-9)
}
