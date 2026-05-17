package domain

import (
	"encoding/json"
	"math"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"
	"crypto-bot/pkg/decmath"
)

const cycleRecordSchemaVersion = 2
const (
	cycleRecordFlowReversion = "reversion"
	cycleRecordFlowTrap      = "trap"
)

// ──────────────────────────────────────────────────────────────────────
// CycleRecordBuilder — accumulates data during a cycle for final persistence.
// ──────────────────────────────────────────────────────────────────────.

// CycleRecordBuilder is a mutable builder that handler methods populate
// during a cycle. At end-of-cycle, Build() produces an immutable CycleRecord.
type CycleRecordBuilder struct {
	mu sync.Mutex

	ReqID      string
	SettleTime time.Time

	// Decision
	FRAtScan               float64
	FRAtRecheck            float64
	Side                   shared.Side
	SafetyPassed           bool
	SafetyRejectReason     string
	AbortReason            string
	AbortFlow              string
	AbortTopic             string
	ErrorFlow              string
	ErrorTopic             string
	ImbalanceFilterEnabled bool
	ImbalanceFilterPassed  bool
	ImbalanceRatio         float64
	ImbalanceNearPct       float64

	// IOC execution
	FireTimestamp time.Time
	IOCIntended   float64
	IOCFillPrice  float64
	IOCFillVol    float64
	IOCFilled     bool
	IOCOrderID    string
	IOCError      string
	LatencyRTTMs  int64

	// TP/SL submitted to exchange
	TPPriceSubmitted float64
	SLPriceSubmitted float64

	// Trap
	TrapEnabled   bool
	TrapOutcome   TrapOutcome
	TrapSkip      TrapSkipReason
	TrapSource    string
	TrapWallPrice float64
	TrapWallOK    bool
	TrapWallAgeMs int64
	TrapWallDist  float64
	TrapPrice     float64
	TrapFilled    bool
	TrapFillPrice float64
	TrapFillVol   float64
	TrapOrderID   string
	TrapError     string
	TrapTPPct     float64
	TrapSLPct     float64
	TrapTPPrice   float64
	TrapSLPrice   float64

	// Exit
	ExitReason string
	ExitTime   time.Time
	Timeout    TimeoutSnapshot
	Cleanup    CleanupSnapshot

	// Trailing
	TrailingActivated   bool
	TrailingActivePrice float64
	TrailingCallbackPct float64

	// Dynamic pricing comparison
	DynamicEnabled bool
	DynamicTPPct   float64
	StaticTPPct    float64
	DynamicSLPct   float64
	StaticSLPct    float64
	ATRValue       float64

	// Market snapshots
	Snapshots []MarketSnapshot

	// MFE/MAE tracking
	Excursion     *ExcursionTracker // legacy alias for IOCExcursion
	IOCExcursion  *ExcursionTracker
	TrapExcursion *ExcursionTracker
}

// NewCycleRecordBuilder creates a fresh builder for a new cycle.
func NewCycleRecordBuilder(reqID string, settle time.Time) *CycleRecordBuilder {
	return &CycleRecordBuilder{
		ReqID:      reqID,
		SettleTime: settle,
	}
}

// Mutate allows thread-safe bulk updates to the builder's fields.
func (b *CycleRecordBuilder) Mutate(fn func(*CycleRecordBuilder)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	fn(b)
}

// AddSnapshot captures market state around a specific event in a thread-safe manner.
func (b *CycleRecordBuilder) AddSnapshot(snap MarketSnapshot) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.Snapshots = append(b.Snapshots, snap)
}

// SetLatencyRTTMs sets the measured latency in a thread-safe manner.
func (b *CycleRecordBuilder) SetLatencyRTTMs(latencyMs int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.LatencyRTTMs = latencyMs
}

// Build assembles the final CycleRecord from accumulated data.
func (b *CycleRecordBuilder) Build(
	symbol string,
	tpPctConfigured float64,
	slPctConfigured float64,
	cfgJSON json.RawMessage,
	timeline []TimelineEntry,
) CycleRecord {
	now := time.Now()

	b.mu.Lock()
	defer b.mu.Unlock()

	// Compute outcome.
	outcome := b.computeOutcome()

	// Compute IOC slippage.
	var slippagePct float64
	if b.IOCIntended > 0 && b.IOCFillPrice > 0 {
		slippagePct = decmath.Mul(
			decmath.Div(math.Abs(decmath.Sub(b.IOCFillPrice, b.IOCIntended)), b.IOCIntended),
			100.0,
		)
	}

	// Compute settle offset.
	var settleOffsetMs int64
	if !b.FireTimestamp.IsZero() {
		settleOffsetMs = b.FireTimestamp.Sub(b.SettleTime).Milliseconds()
	}

	// Compute hold duration.
	var holdDurationMs int64
	if !b.FireTimestamp.IsZero() && !b.ExitTime.IsZero() {
		holdDurationMs = b.ExitTime.Sub(b.FireTimestamp).Milliseconds()
	}

	// Build leg-level excursion data if trackers were active.
	var iocExcursion ExcursionSnapshot
	if b.IOCExcursion != nil {
		iocExcursion = b.IOCExcursion.Snapshot(
			b.Side,
			b.IOCFillPrice,
			tpPctConfigured,
			slPctConfigured,
		)
	}
	var trapExcursion ExcursionSnapshot
	if b.TrapExcursion != nil {
		trapExcursion = b.TrapExcursion.Snapshot(
			b.TrapSide(),
			b.TrapFillPrice,
			b.TrapTPPct,
			b.TrapSLPct,
		)
	}

	rec := CycleRecord{
		SchemaVersion: cycleRecordSchemaVersion,
		ReqID:         b.ReqID,
		Symbol:        symbol,
		SettleTime:    b.SettleTime,
		CreatedAt:     now,
		Flows:         b.flows(),
		Outcome:       outcome,
		AbortReason:   b.AbortReason,
		AbortFlow:     b.AbortFlow,
		AbortTopic:    b.AbortTopic,
		ErrorFlow:     b.ErrorFlow,
		ErrorTopic:    b.ErrorTopic,
		Decision: DecisionSnapshot{
			FRAtScan:               b.FRAtScan,
			FRAtRecheck:            b.FRAtRecheck,
			FRChanged:              b.FRAtRecheck != 0 && b.FRAtScan != 0 && math.Abs(b.FRAtRecheck-b.FRAtScan) > 1e-9,
			Side:                   b.Side,
			SafetyPassed:           b.SafetyPassed,
			SafetyRejectReason:     b.SafetyRejectReason,
			ImbalanceFilterEnabled: b.ImbalanceFilterEnabled,
			ImbalanceFilterPassed:  b.ImbalanceFilterPassed,
			ImbalanceRatio:         b.ImbalanceRatio,
			ImbalanceNearPct:       ratioToPercent(b.ImbalanceNearPct),
		},
		IOC: IOCSnapshot{
			Flow:           cycleRecordFlowReversion,
			IntendedPrice:  b.IOCIntended,
			FillPrice:      b.IOCFillPrice,
			FillVolume:     b.IOCFillVol,
			Filled:         b.IOCFilled,
			SlippagePct:    slippagePct,
			OrderID:        b.IOCOrderID,
			Error:          b.IOCError,
			FireTimestamp:  b.FireTimestamp,
			SettleOffsetMs: settleOffsetMs,
			LatencyRTTMs:   b.LatencyRTTMs,
			Excursion:      iocExcursion,
		},
		Trap: TrapSnapshot{
			Flow:             cycleRecordFlowTrap,
			Enabled:          b.TrapEnabled,
			Outcome:          b.TrapOutcome,
			SkipReason:       b.TrapSkip,
			Source:           b.TrapSource,
			WallPrice:        b.TrapWallPrice,
			WallVerified:     b.TrapWallOK,
			WallAgeMs:        b.TrapWallAgeMs,
			WallDistancePct:  b.TrapWallDist,
			Price:            b.TrapPrice,
			Filled:           b.TrapFilled,
			FillPrice:        b.TrapFillPrice,
			FillVolume:       b.TrapFillVol,
			OrderID:          b.TrapOrderID,
			Error:            b.TrapError,
			TPPctConfigured:  ratioToPercent(b.TrapTPPct),
			SLPctConfigured:  ratioToPercent(b.TrapSLPct),
			TPPriceSubmitted: b.TrapTPPrice,
			SLPriceSubmitted: b.TrapSLPrice,
			Excursion:        trapExcursion,
		},
		Exit: ExitSnapshot{
			Reason:                b.ExitReason,
			HoldDurationMs:        holdDurationMs,
			TPPctConfigured:       ratioToPercent(tpPctConfigured),
			SLPctConfigured:       ratioToPercent(slPctConfigured),
			TPPriceSubmitted:      b.TPPriceSubmitted,
			SLPriceSubmitted:      b.SLPriceSubmitted,
			TrailingActivated:     b.TrailingActivated,
			TrailingActivePrice:   b.TrailingActivePrice,
			TrailingCallbackPct:   ratioToPercent(b.TrailingCallbackPct),
			DynamicPricingEnabled: b.DynamicEnabled,
			DynamicTPPct:          ratioToPercent(b.DynamicTPPct),
			StaticTPPct:           ratioToPercent(b.StaticTPPct),
			DynamicSLPct:          ratioToPercent(b.DynamicSLPct),
			StaticSLPct:           ratioToPercent(b.StaticSLPct),
			ATRValue:              b.ATRValue,
		},
		Timeout:       b.timeoutSnapshot(),
		Cleanup:       b.Cleanup,
		Excursion:     iocExcursion,
		IOCExcursion:  iocExcursion,
		TrapExcursion: trapExcursion,
		Snapshots:     append([]MarketSnapshot(nil), b.Snapshots...),
		Config:        cfgJSON,
		Timeline:      timeline,
	}

	return rec
}

func (b *CycleRecordBuilder) timeoutSnapshot() TimeoutSnapshot {
	timeout := b.Timeout
	if timeout.DurationMs == 0 && timeout.Duration > 0 {
		timeout.DurationMs = timeout.Duration.Milliseconds()
	}
	timeout.Duration = 0
	return timeout
}

func (b *CycleRecordBuilder) flows() []string {
	flows := []string{cycleRecordFlowReversion}
	if b.TrapEnabled || b.TrapOrderID != "" || b.TrapSource != "" || b.TrapFilled || b.TrapOutcome != "" {
		flows = append(flows, cycleRecordFlowTrap)
	}
	return flows
}

func (b *CycleRecordBuilder) TrapSide() shared.Side {
	switch b.Side {
	case shared.SideOpenLong:
		return shared.SideOpenShort
	case shared.SideOpenShort:
		return shared.SideOpenLong
	default:
		return 0
	}
}

func ratioToPercent(v float64) float64 {
	if v == 0 {
		return 0
	}
	return decmath.Mul(v, 100.0)
}

// computeOutcome determines the cycle outcome from the accumulated data.
func (b *CycleRecordBuilder) computeOutcome() CycleOutcome {
	if b.AbortReason != "" {
		return OutcomeAborted
	}
	if b.ExitReason == "timeout" {
		return OutcomeTimeout
	}
	if !b.IOCFilled {
		return OutcomeNoFill
	}
	if b.ExitReason == "sl" {
		return OutcomeLoss
	}
	// Default to profit for any other exit reason (tp, trailing, manual).
	// This is a heuristic — real PnL calculation requires entry/exit prices.
	return OutcomeProfit
}

// ──────────────────────────────────────────────────────────────────────
// ExcursionTracker — tracks MFE/MAE during the hold period.
// ──────────────────────────────────────────────────────────────────────.

// ExcursionTracker tracks the maximum favorable and adverse price excursions
// from a reference entry price during the hold period.
type ExcursionTracker struct {
	side       shared.Side // shared.SideOpenLong or shared.SideOpenShort
	entryPrice float64

	mfePrice float64
	mfeTime  time.Time

	maePrice float64
	maeTime  time.Time
}

// NewExcursionTracker initializes tracking from an entry price.
func NewExcursionTracker(side shared.Side, entryPrice float64) *ExcursionTracker {
	return &ExcursionTracker{
		side:       side,
		entryPrice: entryPrice,
		mfePrice:   entryPrice,
		mfeTime:    time.Now(),
		maePrice:   entryPrice,
		maeTime:    time.Now(),
	}
}

// Update incorporates a new price observation into the MFE/MAE tracking.
func (t *ExcursionTracker) Update(price float64, ts time.Time) {
	if price <= 0 || t.entryPrice <= 0 {
		return
	}

	switch t.side {
	case shared.SideOpenLong:
		// LONG: favorable = higher price, adverse = lower price
		if price > t.mfePrice {
			t.mfePrice = price
			t.mfeTime = ts
		}
		if price < t.maePrice {
			t.maePrice = price
			t.maeTime = ts
		}
	case shared.SideOpenShort:
		// SHORT: favorable = lower price, adverse = higher price
		if price < t.mfePrice || t.mfePrice == t.entryPrice {
			t.mfePrice = price
			t.mfeTime = ts
		}
		if price > t.maePrice {
			t.maePrice = price
			t.maeTime = ts
		}
	default:
		// Excursion tracking is only meaningful for open sides.
	}
}

// Snapshot finalizes the tracked data into an immutable value object and
// calculates ideal outcomes based on configured TP/SL percentages.
func (t *ExcursionTracker) Snapshot(side shared.Side, iocFillPrice, tpPctConfigured, slPctConfigured float64) ExcursionSnapshot {
	if t.entryPrice <= 0 {
		return ExcursionSnapshot{}
	}

	snap := ExcursionSnapshot{
		MFEPrice: t.mfePrice,
		MFETime:  t.mfeTime,
		MAEPrice: t.maePrice,
		MAETime:  t.maeTime,
	}

	switch side {
	case shared.SideOpenLong:
		snap.MFEPct = decmath.Mul(decmath.Div(decmath.Sub(t.mfePrice, t.entryPrice), t.entryPrice), 100.0)
		snap.MAEPct = decmath.Mul(decmath.Div(decmath.Sub(t.entryPrice, t.maePrice), t.entryPrice), 100.0)
	case shared.SideOpenShort:
		snap.MFEPct = decmath.Mul(decmath.Div(decmath.Sub(t.entryPrice, t.mfePrice), t.entryPrice), 100.0)
		snap.MAEPct = decmath.Mul(decmath.Div(decmath.Sub(t.maePrice, t.entryPrice), t.entryPrice), 100.0)
	default:
		// Do nothing
	}

	tpPct := decmath.Mul(tpPctConfigured, 100.0)
	slPct := decmath.Mul(slPctConfigured, 100.0)

	// Compare with configured TP/SL.
	snap.MFEvsTP = decmath.Sub(snap.MFEPct, tpPct)
	snap.MAEvsSL = decmath.Sub(snap.MAEPct, slPct) // Negative = safe, Positive = touched

	// Calculate ideal metrics in hindsight.
	snap.IdealTPPct = decmath.Mul(snap.MFEPct, 0.8)
	snap.IdealSLPct = decmath.Mul(snap.MAEPct, 1.2)
	if snap.MFEPct > 0 {
		snap.TPEfficiency = decmath.Div(tpPct, snap.MFEPct)
		if snap.TPEfficiency > 1.0 {
			// We didn't reach TP.
			if iocFillPrice > 0 {
				var finalPct float64
				if side == shared.SideOpenLong {
					finalPct = decmath.Mul(decmath.Div(decmath.Sub(iocFillPrice, t.entryPrice), t.entryPrice), 100.0)
				} else {
					finalPct = decmath.Mul(decmath.Div(decmath.Sub(t.entryPrice, iocFillPrice), t.entryPrice), 100.0)
				}
				snap.TPEfficiency = decmath.Div(finalPct, snap.MFEPct)
			}
		}
	}
	snap.SLWasTouched = snap.MAEPct >= slPct

	return snap
}
