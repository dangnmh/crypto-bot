package domain

import (
	"context"
	"log/slog"
	"math"
	"sync"
	"time"

	shared "crypto-bot/internal/domain"
)

// DualWallJudge coordinates concurrent evaluation across a fast in-memory Local Model and an external Model Judge (e.g. Ollama),
// returning trusted (true) if either the Local Model OR the external Model Judge evaluates the wall as trusted.
type DualWallJudge struct {
	localJudge    WallJudge
	modelJudge    WallJudge
	shadowTimeout time.Duration
	logger        *slog.Logger
}

// NewDualWallJudge creates a new DualWallJudge with configurable shadow timeout.
func NewDualWallJudge(localJudge, modelJudge WallJudge, shadowTimeout time.Duration, logger *slog.Logger) *DualWallJudge {
	if logger == nil {
		logger = slog.Default()
	}
	if shadowTimeout <= 0 {
		shadowTimeout = 15 * time.Second
	}
	return &DualWallJudge{
		localJudge:    localJudge,
		modelJudge:    modelJudge,
		shadowTimeout: shadowTimeout,
		logger:        logger.With("component", "DualWallJudge"),
	}
}

// JudgeWall evaluates the wall across both local and model judges concurrently, returning trusted if either evaluates as trusted.
func (d *DualWallJudge) JudgeWall(ctx context.Context, wall *Wall, events []WallEvent, trades []shared.PublicTrade) (WallJudgeResult, error) {
	if wall == nil || len(events) == 0 {
		return WallJudgeResult{
			WallID:     "",
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     ReasonEmptyWallOrEvents,
		}, nil
	}

	var (
		localRes     WallJudgeResult
		localErr     error
		localLatency time.Duration

		modelRes     WallJudgeResult
		modelErr     error
		modelLatency time.Duration
	)

	var wg sync.WaitGroup
	wg.Go(func() {
		startLocal := time.Now()
		localRes, localErr = d.localJudge.JudgeWall(ctx, wall, events, trades)
		localLatency = time.Since(startLocal)
	})

	if d.modelJudge != nil {
		wg.Add(1)
		// Deep copy events, trades, and wall pointer reference for concurrent goroutine safety
		eventsCopy := make([]WallEvent, len(events))
		copy(eventsCopy, events)
		tradesCopy := make([]shared.PublicTrade, len(trades))
		copy(tradesCopy, trades)
		wallCopy := *wall

		go func() {
			defer wg.Done()
			shadowCtx, cancel := context.WithTimeout(ctx, d.shadowTimeout)
			defer cancel()

			startModel := time.Now()
			modelRes, modelErr = d.modelJudge.JudgeWall(shadowCtx, &wallCopy, eventsCopy, tradesCopy)
			modelLatency = time.Since(startModel)
		}()
	}

	wg.Wait()

	d.logComparison(ctx, wall, localRes, localErr, localLatency, modelRes, modelErr, modelLatency)

	// Return trusted if either Local OR Model evaluates as trusted
	isTrusted := localRes.IsTrusted || modelRes.IsTrusted

	var combinedScore float64
	var combinedReason string

	switch {
	case localRes.IsTrusted && modelRes.IsTrusted:
		combinedScore = math.Max(localRes.TrustScore, modelRes.TrustScore)
		combinedReason = localRes.Reason
	case modelRes.IsTrusted:
		combinedScore = modelRes.TrustScore
		combinedReason = modelRes.Reason
	case localRes.IsTrusted:
		combinedScore = localRes.TrustScore
		combinedReason = localRes.Reason
	default:
		combinedScore = math.Max(localRes.TrustScore, modelRes.TrustScore)
		if localRes.Reason != "" {
			combinedReason = localRes.Reason
		} else {
			combinedReason = modelRes.Reason
		}
	}

	if localErr != nil && modelErr != nil {
		return WallJudgeResult{
			WallID:     wall.ID,
			TrustScore: 0,
			IsTrusted:  false,
			Reason:     "DUAL_EVAL_ERROR",
		}, localErr
	}

	return WallJudgeResult{
		WallID:     wall.ID,
		TrustScore: combinedScore,
		IsTrusted:  isTrusted,
		Reason:     combinedReason,
	}, nil
}

// EvaluateSync runs both judges synchronously and returns a structured comparison result (useful for benchmarking and tests).
func (d *DualWallJudge) EvaluateSync(ctx context.Context, wall *Wall, events []WallEvent, trades []shared.PublicTrade) (localRes, modelRes WallJudgeResult, isMatch bool, scoreDiff float64, err error) {
	if wall == nil || len(events) == 0 {
		return WallJudgeResult{Reason: ReasonEmptyWallOrEvents}, WallJudgeResult{Reason: ReasonEmptyWallOrEvents}, true, 0, nil
	}

	startLocal := time.Now()
	var localErr error
	localRes, localErr = d.localJudge.JudgeWall(ctx, wall, events, trades)
	localLatency := time.Since(startLocal)

	var modelErr error
	var modelLatency time.Duration
	if d.modelJudge != nil {
		startModel := time.Now()
		modelRes, modelErr = d.modelJudge.JudgeWall(ctx, wall, events, trades)
		modelLatency = time.Since(startModel)
	}

	d.logComparison(ctx, wall, localRes, localErr, localLatency, modelRes, modelErr, modelLatency)

	isMatch = (localRes.IsTrusted == modelRes.IsTrusted)
	scoreDiff = math.Abs(localRes.TrustScore - modelRes.TrustScore)

	if localErr != nil {
		return localRes, modelRes, isMatch, scoreDiff, localErr
	}
	return localRes, modelRes, isMatch, scoreDiff, modelErr
}

func (d *DualWallJudge) logComparison(
	ctx context.Context,
	wall *Wall,
	localRes WallJudgeResult,
	localErr error,
	localLatency time.Duration,
	modelRes WallJudgeResult,
	modelErr error,
	modelLatency time.Duration,
) {
	if modelErr != nil {
		d.logger.WarnContext(ctx, "🤖 Wall Judge Shadow Error",
			slog.String("wall_id", wall.ID),
			slog.String("symbol", wall.Symbol),
			slog.Float64("local_score", localRes.TrustScore),
			slog.Bool("local_trusted", localRes.IsTrusted),
			slog.Duration("local_latency", localLatency),
			slog.String("model_error", modelErr.Error()),
			slog.Duration("model_latency", modelLatency),
		)
		return
	}

	isMatch := (localRes.IsTrusted == modelRes.IsTrusted)
	scoreDiff := math.Abs(localRes.TrustScore - modelRes.TrustScore)

	logLevel := slog.LevelInfo
	if !isMatch {
		logLevel = slog.LevelWarn // Highlight disagreement between local heuristic and model
	}

	d.logger.Log(ctx, logLevel, "🤖 Wall Judge Model Diff",
		slog.String("wall_id", wall.ID),
		slog.String("symbol", wall.Symbol),
		slog.Bool("match", isMatch),
		slog.Float64("score_diff", scoreDiff),
		slog.Float64("local_score", localRes.TrustScore),
		slog.Bool("local_trusted", localRes.IsTrusted),
		slog.Duration("local_latency", localLatency),
		slog.String("local_reason", localRes.Reason),
		slog.Float64("model_score", modelRes.TrustScore),
		slog.Bool("model_trusted", modelRes.IsTrusted),
		slog.Duration("model_latency", modelLatency),
		slog.String("model_reason", modelRes.Reason),
	)
}
