package domain

import (
	"context"

	shared "crypto-bot/internal/domain"
)

// WallJudgeResult contains the evaluation decision and trust metrics produced by a WallJudge.
type WallJudgeResult struct {
	WallID     string  `json:"wall_id"`
	TrustScore float64 `json:"trust_score"`
	IsTrusted  bool    `json:"is_trusted"`
	Reason     string  `json:"reason"`
}

// ReasonEmptyWallOrEvents indicates evaluation was skipped due to nil wall or empty event slice.
const ReasonEmptyWallOrEvents = "EMPTY_WALL_OR_EVENTS"

// WallJudge defines the interface for local rule-based models, ML evaluators, or SLM inference engines.
type WallJudge interface {
	JudgeWall(ctx context.Context, wall *Wall, events []WallEvent, trades []shared.PublicTrade) (WallJudgeResult, error)
}
