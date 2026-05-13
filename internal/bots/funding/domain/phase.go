package domain

// Phase represents a specific stage or tag in the cycle's lifecycle.
type Phase string

const (
	PhaseScan     Phase = "scan"
	PhaseRecheck  Phase = "recheck"
	PhaseArm      Phase = "arm"
	PhaseFire     Phase = "fire"
	PhaseIOC      Phase = "ioc"
	PhaseTrap     Phase = "trap"
	PhaseTrailing Phase = "trailing"
	PhaseAbort    Phase = "abort"

	PhaseScanning  Phase = "SCANNING"
	PhaseArmed     Phase = "ARMED"
	PhaseFiredIOC  Phase = "FIRED_IOC"
	PhaseFiredTrap Phase = "FIRED_TRAP"
)
