package domain_test

import (
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWall_ProjectFromEvents(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 8, 23, 12, 0, 0, 0, time.UTC)
	t1 := now.Add(1 * time.Second)
	t2 := now.Add(2 * time.Second)
	t3 := now.Add(3 * time.Second)
	t4 := now.Add(4 * time.Second)

	events := []pjdomain.WallEvent{
		{
			WallID:        "wall-123",
			Seq:           1,
			Timestamp:     now,
			EventType:     pjdomain.WallEventBorn,
			Volume:        100.0,
			DistancePct:   0.2,
			SpreadPct:     0.05,
			RelativeRatio: 25.0,
		},
		{
			WallID:      "wall-123",
			Seq:         2,
			Timestamp:   t1,
			EventType:   pjdomain.WallEventMatured,
			Volume:      100.0,
			DistancePct: 0.2,
		},
		{
			WallID:      "wall-123",
			Seq:         3,
			Timestamp:   t2,
			EventType:   pjdomain.WallEventResized,
			Volume:      80.0,
			DeltaVolume: -20.0,
			DistancePct: 0.2,
		},
		{
			WallID:      "wall-123",
			Seq:         4,
			Timestamp:   t3,
			EventType:   pjdomain.WallEventResized,
			Volume:      120.0,
			DeltaVolume: 40.0,
			DistancePct: 0.2,
		},
		{
			WallID:      "wall-123",
			Seq:         5,
			Timestamp:   t4,
			EventType:   pjdomain.WallEventDisappeared,
			Volume:      120.0,
			DistancePct: 0.2,
		},
	}

	wall := pjdomain.ProjectWallFromEvents(events)
	require.NotNil(t, wall)
	assert.Equal(t, "wall-123", wall.ID)
	assert.Equal(t, 100.0, wall.InitialVolume)
	assert.Equal(t, 120.0, wall.Volume)
	assert.Equal(t, pjdomain.WallStatusDisappeared, wall.Status)
	assert.Equal(t, int64(5), wall.EventSeq)
	assert.Equal(t, now, wall.FirstDetectedAt)
	require.NotNil(t, wall.DisappearedAt)
	assert.Equal(t, t4, *wall.DisappearedAt)
	assert.Equal(t, 4*time.Second, wall.GetAgeAt(t4))

	// Aggregate dynamic helper metrics derived from event stream
	assert.Equal(t, 2, pjdomain.CalculateResizeCount(events))
}
