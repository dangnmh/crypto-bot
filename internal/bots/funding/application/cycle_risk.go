package application

import (
	"fmt"

	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/pkg/decmath"
)

func (o *CycleOrchestrator) cycleRiskAllowsReversion(c domain.Candidate) error {
	return o.checkCycleRisk(c.ReversionNotionalUSDT(), 0, false)
}

func (o *CycleOrchestrator) cycleRiskAllowsTrap(c domain.Candidate, trapNotional float64) error {
	return o.checkCycleRisk(c.ReversionNotionalUSDT(), trapNotional, true)
}

func (o *CycleOrchestrator) checkCycleRisk(reversionNotional, trapNotional float64, includeTrap bool) error {
	safety := o.global.System.Safety

	cycleNotional := reversionNotional
	if includeTrap {
		cycleNotional = decmath.Add(cycleNotional, trapNotional)
	}
	if safety.MaxCycleNotionalUSDT > 0 && cycleNotional > safety.MaxCycleNotionalUSDT {
		return fmt.Errorf("cycle notional %.4f exceeds max %.4f", cycleNotional, safety.MaxCycleNotionalUSDT)
	}

	reversionLoss := decmath.Mul(reversionNotional, o.cfg.FundingReversion.StopLossPct)
	cycleLoss := reversionLoss
	if includeTrap {
		cycleLoss = decmath.Add(cycleLoss, decmath.Mul(trapNotional, o.cfg.FundingTrap.StopLossPct))
	}
	if safety.MaxCycleLossUSDT > 0 && cycleLoss > safety.MaxCycleLossUSDT {
		return fmt.Errorf("cycle loss %.4f exceeds max %.4f", cycleLoss, safety.MaxCycleLossUSDT)
	}

	return nil
}
