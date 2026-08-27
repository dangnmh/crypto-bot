package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockWallJudge struct {
	result pjdomain.WallJudgeResult
	err    error
	delay  time.Duration
}

func (m *mockWallJudge) JudgeWall(_ context.Context, _ *pjdomain.Wall, _ []pjdomain.WallEvent, _ []shared.PublicTrade) (pjdomain.WallJudgeResult, error) {
	if m.delay > 0 {
		time.Sleep(m.delay)
	}
	return m.result, m.err
}

func TestDualWallJudge_EvaluationOrLogic(t *testing.T) {
	t.Parallel()

	wall := &pjdomain.Wall{
		ID:     "wall-1",
		Symbol: "BTCUSDT",
		Side:   shared.SideOpenLong,
		Price:  60000.0,
	}
	events := []pjdomain.WallEvent{{WallID: "wall-1", Seq: 1, EventType: pjdomain.WallEventBorn}}

	tests := []struct {
		name         string
		localResult  pjdomain.WallJudgeResult
		modelResult  pjdomain.WallJudgeResult
		expectTrust  bool
		expectScore  float64
		expectReason string
	}{
		{
			name:         "returns trusted when local is true and model is false",
			localResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.85, IsTrusted: true, Reason: "LOCAL_ABSORBED"},
			modelResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.30, IsTrusted: false, Reason: "MODEL_SPOOF"},
			expectTrust:  true,
			expectScore:  0.85,
			expectReason: "LOCAL_ABSORBED",
		},
		{
			name:         "returns trusted when local is false and model is true",
			localResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.40, IsTrusted: false, Reason: "LOCAL_LOW_ABSORPTION"},
			modelResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.90, IsTrusted: true, Reason: "MODEL_STRUCTURAL_DEPTH"},
			expectTrust:  true,
			expectScore:  0.90,
			expectReason: "MODEL_STRUCTURAL_DEPTH",
		},
		{
			name:         "returns untrusted when both local and model are false",
			localResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.30, IsTrusted: false, Reason: "LOCAL_REJECT"},
			modelResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.20, IsTrusted: false, Reason: "MODEL_REJECT"},
			expectTrust:  false,
			expectScore:  0.30,
			expectReason: "LOCAL_REJECT",
		},
		{
			name:         "returns max score when both local and model are true",
			localResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.80, IsTrusted: true, Reason: "LOCAL_ABSORBED"},
			modelResult:  pjdomain.WallJudgeResult{WallID: "wall-1", TrustScore: 0.95, IsTrusted: true, Reason: "MODEL_CONFIRMED"},
			expectTrust:  true,
			expectScore:  0.95,
			expectReason: "LOCAL_ABSORBED",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			localJudge := &mockWallJudge{result: tt.localResult}
			modelJudge := &mockWallJudge{result: tt.modelResult}
			dualJudge := pjdomain.NewDualWallJudge(localJudge, modelJudge, 0, nil)

			res, err := dualJudge.JudgeWall(context.Background(), wall, events, nil)
			require.NoError(t, err)
			assert.Equal(t, tt.expectTrust, res.IsTrusted)
			assert.InDelta(t, tt.expectScore, res.TrustScore, 1e-6)
			assert.Equal(t, tt.expectReason, res.Reason)
		})
	}
}

func TestDualWallJudge_EvaluateSync(t *testing.T) {
	t.Parallel()

	localJudge := &mockWallJudge{
		result: pjdomain.WallJudgeResult{
			WallID:     "wall-1",
			TrustScore: 0.85,
			IsTrusted:  true,
			Reason:     "LOCAL_OK",
		},
	}

	modelJudge := &mockWallJudge{
		result: pjdomain.WallJudgeResult{
			WallID:     "wall-1",
			TrustScore: 0.40,
			IsTrusted:  false,
			Reason:     "MODEL_SPOOF_RISK",
		},
	}

	dualJudge := pjdomain.NewDualWallJudge(localJudge, modelJudge, 0, nil)

	wall := &pjdomain.Wall{ID: "wall-1", Symbol: "BTCUSDT"}
	events := []pjdomain.WallEvent{{WallID: "wall-1", Seq: 1}}

	localRes, modelRes, isMatch, scoreDiff, err := dualJudge.EvaluateSync(context.Background(), wall, events, nil)
	require.NoError(t, err)
	assert.False(t, isMatch)
	assert.InDelta(t, 0.45, scoreDiff, 1e-4)
	assert.True(t, localRes.IsTrusted)
	assert.False(t, modelRes.IsTrusted)
}

func TestDualWallJudge_ModelErrorDoesNotBreakLocal(t *testing.T) {
	t.Parallel()

	localJudge := &mockWallJudge{
		result: pjdomain.WallJudgeResult{
			WallID:     "wall-1",
			TrustScore: 0.90,
			IsTrusted:  true,
			Reason:     "LOCAL_OK",
		},
	}

	modelJudge := &mockWallJudge{
		err: errors.New("network timeout"),
	}

	dualJudge := pjdomain.NewDualWallJudge(localJudge, modelJudge, 0, nil)

	wall := &pjdomain.Wall{ID: "wall-1", Symbol: "BTCUSDT"}
	events := []pjdomain.WallEvent{{WallID: "wall-1", Seq: 1}}

	res, err := dualJudge.JudgeWall(context.Background(), wall, events, nil)
	require.NoError(t, err)
	assert.True(t, res.IsTrusted)
	assert.Equal(t, 0.90, res.TrustScore)
}
