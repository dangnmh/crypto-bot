package domain

import (
	"context"
	"fmt"
	"math"

	shared "crypto-bot/internal/domain"
)

// DefaultWallJudgeConfig defines thresholds and weights for the default rule-based model.
type DefaultWallJudgeConfig struct {
	MinTrustScore float64 `json:"min_trust_score"` // default: 0.70 (70%)
}

// DefaultWallJudge evaluates wall event streams with quantitative factor weighting.
type DefaultWallJudge struct {
	cfg DefaultWallJudgeConfig
}

// NewDefaultWallJudge creates a new DefaultWallJudge.
func NewDefaultWallJudge(cfg DefaultWallJudgeConfig) *DefaultWallJudge {
	if cfg.MinTrustScore <= 0 {
		cfg.MinTrustScore = 0.70
	}
	return &DefaultWallJudge{cfg: cfg}
}

// JudgeWall evaluates the point-in-time state of the wall, event stream, and trade tape.
func (j *DefaultWallJudge) JudgeWall(_ context.Context, wall *Wall, events []WallEvent, trades []shared.PublicTrade) (WallJudgeResult, error) {
	if wall == nil || len(events) == 0 {
		return WallJudgeResult{
			WallID:     "",
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     ReasonEmptyWallOrEvents,
		}, nil
	}

	metrics := ReconcileWallData(wall, events, trades)

	// 1. Age Factor (Weight: 20%)
	latestEvent := events[len(events)-1]
	age := wall.GetAgeAt(latestEvent.Timestamp)
	ageScore := scoreAge(age.Seconds())

	// 2. Size Ratio Factor (Weight: 15%)
	sizeScore := scoreSizeRatio(wall.RelativeRatio)

	// 3. Absorption Factor (Weight: 30%)
	absorptionRatio := 0.0
	if wall.InitialVolume > 0 {
		absorptionRatio = metrics.AbsorbedVolume / wall.InitialVolume
	}
	absorptionScore := scoreAbsorption(absorptionRatio)

	// 4. Stability Factor (Weight: 20%)
	stabilityScore := scoreStability(metrics.ResizeCount)

	// 5. Spread/Distance Factor (Weight: 15%)
	spreadScore := scoreSpreadAndDistance(latestEvent.SpreadPct, latestEvent.DistancePct)

	// Weighted Trust Score
	trustScore := (0.20 * ageScore) +
		(0.15 * sizeScore) +
		(0.30 * absorptionScore) +
		(0.20 * stabilityScore) +
		(0.15 * spreadScore)

	// Clamp to [0.0, 1.0]
	trustScore = math.Max(0.0, math.Min(1.0, trustScore))
	isTrusted := trustScore >= j.cfg.MinTrustScore

	reason := fmt.Sprintf("SCORE_%.2f_AGE_%.1fs_ABS_%.1f%%_RES_%d",
		trustScore, age.Seconds(), absorptionRatio*100.0, metrics.ResizeCount)

	return WallJudgeResult{
		WallID:     wall.ID,
		TrustScore: trustScore,
		IsTrusted:  isTrusted,
		Reason:     reason,
	}, nil
}

func scoreAge(seconds float64) float64 {
	switch {
	case seconds < 1.0:
		return 0.0
	case seconds < 3.0:
		return 0.3
	case seconds < 10.0:
		return 0.6
	case seconds < 30.0:
		return 0.85
	default:
		return 1.0
	}
}

func scoreSizeRatio(ratio float64) float64 {
	switch {
	case ratio < 5.0:
		return 0.0
	case ratio < 10.0:
		return 0.4
	case ratio < 20.0:
		return 0.7
	case ratio <= 100.0:
		return 1.0
	default:
		return 0.75 // Oversized spoof risk penalty
	}
}

func scoreAbsorption(ratio float64) float64 {
	switch {
	case ratio < 0.01:
		return 0.2
	case ratio < 0.05:
		return 0.5
	case ratio < 0.15:
		return 0.8
	default:
		return 1.0
	}
}

func scoreStability(resizes int) float64 {
	switch resizes {
	case 0:
		return 1.0
	case 1:
		return 0.8
	case 2:
		return 0.5
	default:
		return 0.2
	}
}

func scoreSpreadAndDistance(spreadPct, distPct float64) float64 {
	if spreadPct <= 0.2 && distPct <= 0.5 {
		return 1.0
	}
	if spreadPct <= 0.5 && distPct <= 1.0 {
		return 0.8
	}
	return 0.5
}
