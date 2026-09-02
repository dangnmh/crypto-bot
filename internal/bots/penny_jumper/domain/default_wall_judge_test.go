package domain_test

import (
	"context"
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultWallJudge_EmptyInputs(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})

	res, err := judge.JudgeWall(context.Background(), nil, nil, nil)
	require.NoError(t, err)
	assert.False(t, res.IsTrusted)
	assert.Equal(t, "EMPTY_WALL_OR_EVENTS", res.Reason)
}

func TestDefaultWallJudge_GenuineAbsorbingWall(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	wall := &pjdomain.Wall{
		ID:              "genuine-wall-1",
		Exchange:        "toobit",
		Symbol:          "BTCUSDT",
		Side:            shared.SideOpenLong,
		Price:           60000.0,
		Volume:          80.0,
		InitialVolume:   100.0,
		RelativeRatio:   35.0,
		FirstDetectedAt: now,
		Status:          pjdomain.WallStatusActive,
	}

	events := []pjdomain.WallEvent{
		{
			WallID:        "genuine-wall-1",
			Seq:           1,
			Timestamp:     now,
			EventType:     pjdomain.WallEventBorn,
			Volume:        100.0,
			DistancePct:   0.2,
			SpreadPct:     0.1,
			RelativeRatio: 35.0,
		},
		{
			WallID:      "genuine-wall-1",
			Seq:         2,
			Timestamp:   now.Add(10 * time.Second),
			EventType:   pjdomain.WallEventMatured,
			Volume:      100.0,
			DistancePct: 0.2,
			SpreadPct:   0.1,
		},
		{
			WallID:      "genuine-wall-1",
			Seq:         3,
			Timestamp:   now.Add(35 * time.Second),
			EventType:   pjdomain.WallEventResized,
			Volume:      80.0,
			DeltaVolume: -20.0,
			DistancePct: 0.1,
			SpreadPct:   0.05,
		},
	}

	trades := []shared.PublicTrade{
		{
			Symbol:    "BTCUSDT",
			Price:     60000.0,
			Volume:    20.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(35 * time.Second),
		},
	}

	res, err := judge.JudgeWall(context.Background(), wall, events, trades)
	require.NoError(t, err)
	assert.True(t, res.IsTrusted)
	assert.GreaterOrEqual(t, res.TrustScore, 0.75)
	assert.Contains(t, res.Reason, "ABS_20.0%")
}

func TestDefaultWallJudge_SpoofFlickeringWall(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	wall := &pjdomain.Wall{
		ID:              "flicker-wall-1",
		Exchange:        "toobit",
		Symbol:          "BTCUSDT",
		Side:            shared.SideOpenLong,
		Price:           60000.0,
		Volume:          100.0,
		InitialVolume:   100.0,
		RelativeRatio:   25.0,
		FirstDetectedAt: now,
		Status:          pjdomain.WallStatusActive,
	}

	// Wall constantly resizes up/down within 0.5s without any absorption (highly unstable spoof)
	events := []pjdomain.WallEvent{
		{WallID: "flicker-wall-1", Seq: 1, Timestamp: now, EventType: pjdomain.WallEventBorn, Volume: 100.0, DistancePct: 0.2, RelativeRatio: 25.0},
		{WallID: "flicker-wall-1", Seq: 2, Timestamp: now.Add(100 * time.Millisecond), EventType: pjdomain.WallEventResized, Volume: 80.0, DistancePct: 0.2},
		{WallID: "flicker-wall-1", Seq: 3, Timestamp: now.Add(200 * time.Millisecond), EventType: pjdomain.WallEventResized, Volume: 120.0, DistancePct: 0.2},
		{WallID: "flicker-wall-1", Seq: 4, Timestamp: now.Add(300 * time.Millisecond), EventType: pjdomain.WallEventResized, Volume: 90.0, DistancePct: 0.2},
		{WallID: "flicker-wall-1", Seq: 5, Timestamp: now.Add(400 * time.Millisecond), EventType: pjdomain.WallEventResized, Volume: 110.0, DistancePct: 0.2},
	}

	res, err := judge.JudgeWall(context.Background(), wall, events, nil)
	require.NoError(t, err)
	assert.False(t, res.IsTrusted)
	assert.Less(t, res.TrustScore, 0.70)
}

func TestDefaultWallJudge_FarDistanceSpoofWall(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	wall := &pjdomain.Wall{
		ID:              "far-wall-1",
		Exchange:        "toobit",
		Symbol:          "BTCUSDT",
		Side:            shared.SideOpenLong,
		Price:           50000.0,
		Volume:          100.0,
		InitialVolume:   100.0,
		DistancePct:     4.5, // > 3.0% far spoof
		RelativeRatio:   25.0,
		FirstDetectedAt: now,
		Status:          pjdomain.WallStatusActive,
	}

	events := []pjdomain.WallEvent{
		{WallID: "far-wall-1", Seq: 1, Timestamp: now, EventType: pjdomain.WallEventBorn, Volume: 100.0, DistancePct: 4.5, RelativeRatio: 25.0},
		{WallID: "far-wall-1", Seq: 2, Timestamp: now.Add(15 * time.Second), EventType: pjdomain.WallEventMatured, Volume: 100.0, DistancePct: 4.5},
	}

	res, err := judge.JudgeWall(context.Background(), wall, events, nil)
	require.NoError(t, err)
	assert.False(t, res.IsTrusted)
	assert.Less(t, res.TrustScore, 0.70)
}

func TestDefaultWallJudge_AbsorptionVsPullComparison(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	// Wall 1: Legitimate Wall Absorbed by Market Takers (100 -> 80 with 20 volume in trades)
	wallAbsorbed := &pjdomain.Wall{
		ID:              "wall-absorbed",
		Exchange:        "toobit",
		Symbol:          "BTCUSDT",
		Side:            shared.SideOpenLong,
		Price:           60000.0,
		Volume:          80.0,
		InitialVolume:   100.0,
		RelativeRatio:   25.0,
		FirstDetectedAt: now,
		Status:          pjdomain.WallStatusActive,
	}
	eventsAbsorbed := []pjdomain.WallEvent{
		{
			WallID:        "wall-absorbed",
			Seq:           1,
			Timestamp:     now,
			EventType:     pjdomain.WallEventBorn,
			Volume:        100.0,
			DistancePct:   0.3,
			SpreadPct:     0.1,
			RelativeRatio: 25.0,
		},
		{
			WallID:      "wall-absorbed",
			Seq:         2,
			Timestamp:   now.Add(15 * time.Second),
			EventType:   pjdomain.WallEventMatured,
			Volume:      100.0,
			DistancePct: 0.3,
			SpreadPct:   0.1,
		},
		{
			WallID:      "wall-absorbed",
			Seq:         3,
			Timestamp:   now.Add(20 * time.Second),
			EventType:   pjdomain.WallEventResized,
			Volume:      80.0,
			DeltaVolume: -20.0,
			DistancePct: 0.3,
			SpreadPct:   0.1,
		},
	}
	tradesAbsorbed := []shared.PublicTrade{
		{
			Symbol:    "BTCUSDT",
			Price:     60000.0,
			Volume:    20.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(20 * time.Second),
		},
	}

	// Wall 2: Phantom Pull Wall (100 -> 80 via maker cancellation / resize down, 0 trades)
	wallPulled := &pjdomain.Wall{
		ID:              "wall-pulled",
		Exchange:        "toobit",
		Symbol:          "BTCUSDT",
		Side:            shared.SideOpenLong,
		Price:           60000.0,
		Volume:          80.0,
		InitialVolume:   100.0,
		RelativeRatio:   25.0,
		FirstDetectedAt: now,
		Status:          pjdomain.WallStatusActive,
	}
	eventsPulled := []pjdomain.WallEvent{
		{
			WallID:        "wall-pulled",
			Seq:           1,
			Timestamp:     now,
			EventType:     pjdomain.WallEventBorn,
			Volume:        100.0,
			DistancePct:   0.3,
			SpreadPct:     0.1,
			RelativeRatio: 25.0,
		},
		{
			WallID:      "wall-pulled",
			Seq:         2,
			Timestamp:   now.Add(15 * time.Second),
			EventType:   pjdomain.WallEventMatured,
			Volume:      100.0,
			DistancePct: 0.3,
			SpreadPct:   0.1,
		},
		{
			WallID:      "wall-pulled",
			Seq:         3,
			Timestamp:   now.Add(20 * time.Second),
			EventType:   pjdomain.WallEventResized,
			Volume:      80.0,
			DeltaVolume: -20.0,
			DistancePct: 0.3,
			SpreadPct:   0.1,
		},
	}

	resAbsorbed, err := judge.JudgeWall(context.Background(), wallAbsorbed, eventsAbsorbed, tradesAbsorbed)
	require.NoError(t, err)

	resPulled, err := judge.JudgeWall(context.Background(), wallPulled, eventsPulled, nil)
	require.NoError(t, err)

	assert.True(t, resAbsorbed.IsTrusted)
	assert.Greater(t, resAbsorbed.TrustScore, resPulled.TrustScore, "Wall with true absorption should score significantly higher than wall with phantom pulls")
	assert.Equal(t, 20.0, pjdomain.ReconcileWallData(wallAbsorbed, eventsAbsorbed, tradesAbsorbed).AbsorbedVolume)
	assert.Equal(t, 0.0, pjdomain.ReconcileWallData(wallPulled, eventsPulled, nil).AbsorbedVolume)
}

func TestDefaultWallJudge_MicrostructureFactors(t *testing.T) {
	t.Parallel()

	judge := pjdomain.NewDefaultWallJudge(pjdomain.DefaultWallJudgeConfig{MinTrustScore: 0.70})
	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)

	baseEvents := []pjdomain.WallEvent{
		{WallID: "w-base", Seq: 1, Timestamp: now, EventType: pjdomain.WallEventBorn, Volume: 50.0, DistancePct: 0.1, RelativeRatio: 20.0, SpreadPct: 0.05},
		{WallID: "w-base", Seq: 2, Timestamp: now.Add(12 * time.Second), EventType: pjdomain.WallEventMatured, Volume: 50.0, DistancePct: 0.1, SpreadPct: 0.05},
	}

	makeWall := func(imb, backing float64, pulls, fills int) *pjdomain.Wall {
		return &pjdomain.Wall{
			ID:              "w-test",
			Exchange:        "mexc_spot",
			Symbol:          "SOLUSDT",
			Side:            shared.SideOpenLong,
			Price:           150.0,
			Volume:          50.0,
			InitialVolume:   50.0,
			RelativeRatio:   20.0,
			DepthImbalance:  imb,
			BackingRatio:    backing,
			PullCount1h:     pulls,
			FillCount1h:     fills,
			FirstDetectedAt: now,
			Status:          pjdomain.WallStatusActive,
		}
	}

	// 1. Depth Imbalance & Backing effect
	resHighImb, err := judge.JudgeWall(context.Background(), makeWall(3.5, 1.8, 0, 0), baseEvents, nil)
	require.NoError(t, err)

	resLowImb, err := judge.JudgeWall(context.Background(), makeWall(0.3, 0.1, 0, 0), baseEvents, nil)
	require.NoError(t, err)

	assert.Greater(t, resHighImb.TrustScore, resLowImb.TrustScore, "High imbalance & backing must score higher than low imbalance & backing")

	// 2. Spoofing History Penalty effect
	resClean, err := judge.JudgeWall(context.Background(), makeWall(1.0, 1.0, 0, 2), baseEvents, nil)
	require.NoError(t, err)

	resToxic, err := judge.JudgeWall(context.Background(), makeWall(1.0, 1.0, 4, 0), baseEvents, nil)
	require.NoError(t, err)

	assert.Greater(t, resClean.TrustScore, resToxic.TrustScore, "Clean history with fills must score higher than 4 pulls spoof history")
}
