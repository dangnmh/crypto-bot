package domain

// TrapOutcome enumerates terminal or intermediate outcomes for the Trap leg.
type TrapOutcome string

const (
	TrapOutcomePlaced  TrapOutcome = "placed"
	TrapOutcomeFilled  TrapOutcome = "filled"
	TrapOutcomeClosed  TrapOutcome = "closed"
	TrapOutcomeTimeout TrapOutcome = "timeout"
	TrapOutcomeAborted TrapOutcome = "aborted"
	TrapOutcomeSkipped TrapOutcome = "skipped"
)

// TrapSkipReason explains why a Trap leg ended before order placement.
type TrapSkipReason string

const (
	TrapSkipReasonWallNotVerified  TrapSkipReason = "wall_not_verified"
	TrapSkipReasonInvalidPrice     TrapSkipReason = "invalid_price"
	TrapSkipReasonInvalidVolume    TrapSkipReason = "invalid_volume"
	TrapSkipReasonCycleRiskBlocked TrapSkipReason = "cycle_risk_blocked"
	TrapSkipReasonOrderFailed      TrapSkipReason = "order_failed"
)

// CycleOutcome enumerates possible cycle endings.
type CycleOutcome string

const (
	OutcomeProfit  CycleOutcome = "profit"
	OutcomeLoss    CycleOutcome = "loss"
	OutcomeAborted CycleOutcome = "aborted"
	OutcomeTimeout CycleOutcome = "timeout"
	OutcomeNoFill  CycleOutcome = "no_fill"
)
