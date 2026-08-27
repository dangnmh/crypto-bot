package persistence_test

import (
	"context"
	"testing"
	"time"

	pjdomain "crypto-bot/internal/bots/penny_jumper/domain"
	"crypto-bot/internal/bots/penny_jumper/infrastructure/persistence"
	shared "crypto-bot/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestGormWallRepository_NilDB(t *testing.T) {
	t.Parallel()

	repo := persistence.NewGormWallRepository(nil)
	ctx := context.Background()

	require.NoError(t, repo.Save(ctx, &persistence.PennyJumperWallRecord{ID: "w1"}))

	rec, err := repo.FindByID(ctx, "w1")
	require.NoError(t, err)
	assert.Nil(t, rec)

	list, err := repo.List(ctx, "BTCUSDT", 10)
	require.NoError(t, err)
	assert.Nil(t, list)
}

func TestGormWallRepository_CRUD(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(&persistence.PennyJumperWallRecord{}))
	repo := persistence.NewGormWallRepository(db)
	ctx := context.Background()

	now := time.Now().Truncate(time.Millisecond)
	completed := now.Add(10 * time.Second)

	events := []pjdomain.WallEvent{
		{
			WallID:    "req-wall-1",
			Seq:       1,
			EventType: pjdomain.WallEventBorn,
			Volume:    100.0,
			Timestamp: now,
		},
		{
			WallID:      "req-wall-1",
			Seq:         2,
			EventType:   pjdomain.WallEventResized,
			Volume:      80.0,
			DeltaVolume: -20.0,
			Timestamp:   now.Add(5 * time.Second),
		},
	}

	trades := []shared.PublicTrade{
		{
			Symbol:    "BTCUSDT",
			Price:     50000.0,
			Volume:    20.0,
			Side:      shared.SideOpenShort,
			Timestamp: now.Add(3 * time.Second),
		},
	}

	record := &persistence.PennyJumperWallRecord{
		ID:            "req-wall-1",
		Exchange:      "toobit",
		Symbol:        "BTCUSDT",
		Side:          "LONG",
		WallPrice:     50000.0,
		InitialVolume: 100.0,
		FinalVolume:   80.0,
		DistancePct:   0.2,
		SpreadPct:     0.05,
		Outcome:       "ACTIVE",
		Reason:        "WALL_DETECTED",
		DurationMs:    5000,
		CreatedAt:     now,
		CompletedAt:   &completed,
	}
	require.NoError(t, record.SetEvents(events))
	require.NoError(t, record.SetTrades(trades))

	// Save (Insert)
	err = repo.Save(ctx, record)
	require.NoError(t, err)

	// FindByID
	found, err := repo.FindByID(ctx, "req-wall-1")
	require.NoError(t, err)
	require.NotNil(t, found)
	assert.Equal(t, "BTCUSDT", found.Symbol)
	assert.Equal(t, "toobit", found.Exchange)
	assert.Equal(t, 50000.0, found.WallPrice)
	assert.Equal(t, 100.0, found.InitialVolume)
	assert.Equal(t, 80.0, found.FinalVolume)

	loadedEvents, err := found.GetEvents()
	require.NoError(t, err)
	require.Len(t, loadedEvents, 2)
	assert.Equal(t, pjdomain.WallEventBorn, loadedEvents[0].EventType)
	assert.Equal(t, pjdomain.WallEventResized, loadedEvents[1].EventType)

	loadedTrades, err := found.GetTrades()
	require.NoError(t, err)
	require.Len(t, loadedTrades, 1)
	assert.Equal(t, 50000.0, loadedTrades[0].Price)
	assert.Equal(t, 20.0, loadedTrades[0].Volume)

	// Update (Upsert)
	found.Outcome = "DISAPPEARED"
	found.FinalVolume = 0
	err = repo.Save(ctx, found)
	require.NoError(t, err)

	updated, err := repo.FindByID(ctx, "req-wall-1")
	require.NoError(t, err)
	require.NotNil(t, updated)
	assert.Equal(t, "DISAPPEARED", updated.Outcome)
	assert.Equal(t, 0.0, updated.FinalVolume)

	// List
	list, err := repo.List(ctx, "BTCUSDT", 10)
	require.NoError(t, err)
	assert.Len(t, list, 1)

	// TableName
	assert.Equal(t, "penny_jumper_walls", persistence.PennyJumperWallRecord{}.TableName())
}
