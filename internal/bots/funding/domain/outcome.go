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
	TrapSkipReasonWallNotVerified TrapSkipReason = "wall_not_verified"
	TrapSkipReasonInvalidPrice    TrapSkipReason = "invalid_price"
	TrapSkipReasonInvalidVolume   TrapSkipReason = "invalid_volume"
	TrapSkipReasonRiskBlocked     TrapSkipReason = "risk_blocked"
	TrapSkipReasonOrderFailed     TrapSkipReason = "order_failed"
)

// Outcome enumerates possible endings.
type Outcome string

const (
	OutcomeProfit  Outcome = "profit"
	OutcomeLoss    Outcome = "loss"
	OutcomeAborted Outcome = "aborted"
	OutcomeTimeout Outcome = "timeout"
	OutcomeNoFill  Outcome = "no_fill"
)
