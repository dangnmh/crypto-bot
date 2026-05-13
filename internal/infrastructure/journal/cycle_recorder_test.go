package journal_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/domain"
	shared "crypto-bot/internal/domain"
	"crypto-bot/internal/infrastructure/journal"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJSONLCycleRecorder_Record(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec, err := journal.NewJSONLCycleRecorder(dir)
	require.NoError(t, err)
	defer func() { _ = rec.Close() }()

	record := domain.CycleRecord{
		ReqID:      "test-123",
		Symbol:     "BTC_USDT",
		SettleTime: time.Date(2026, 5, 12, 16, 0, 0, 0, time.UTC),
		CreatedAt:  time.Now(),
		Outcome:    domain.OutcomeProfit,
		Decision: domain.DecisionSnapshot{
			FRAtScan: 0.007,
			Side:     shared.SideOpenLong,
		},
		IOC: domain.IOCSnapshot{
			Filled:    true,
			FillPrice: 100.5,
		},
	}

	err = rec.Record(context.Background(), record)
	require.NoError(t, err)

	// Verify file contents.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)
	assert.True(t, strings.HasPrefix(files[0].Name(), "cycles-"))
	assert.True(t, strings.HasSuffix(files[0].Name(), ".jsonl"))

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)

	// Should be valid JSON.
	var decoded domain.CycleRecord
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)
	assert.Equal(t, "test-123", decoded.ReqID)
	assert.Equal(t, "BTC_USDT", decoded.Symbol)
	assert.Equal(t, domain.OutcomeProfit, decoded.Outcome)
	assert.InDelta(t, 0.007, decoded.Decision.FRAtScan, 1e-9)
	assert.Equal(t, shared.SideOpenLong, decoded.Decision.Side)
	assert.True(t, decoded.IOC.Filled)
	assert.InDelta(t, 100.5, decoded.IOC.FillPrice, 1e-9)
}

func TestJSONLCycleRecorder_MultipleRecords(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec, err := journal.NewJSONLCycleRecorder(dir)
	require.NoError(t, err)
	defer func() { _ = rec.Close() }()

	now := time.Now()
	for i := range 3 {
		record := domain.CycleRecord{
			ReqID:     "req-" + string(rune('A'+i)),
			Symbol:    "ETH_USDT",
			CreatedAt: now,
			Outcome:   domain.OutcomeProfit,
		}
		require.NoError(t, rec.Record(context.Background(), record))
	}

	// Verify file has 3 lines.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, 3)

	// Each line should be valid JSON.
	for _, line := range lines {
		var rec domain.CycleRecord
		require.NoError(t, json.Unmarshal([]byte(line), &rec))
	}
}

func TestJSONLCycleRecorder_CreatesDirIfNotExists(t *testing.T) {
	t.Parallel()

	dir := filepath.Join(t.TempDir(), "nested", "deep", "journal")
	rec, err := journal.NewJSONLCycleRecorder(dir)
	require.NoError(t, err)
	defer func() { _ = rec.Close() }()

	require.NoError(t, rec.Record(context.Background(), domain.CycleRecord{
		ReqID:     "test",
		CreatedAt: time.Now(),
		Outcome:   domain.OutcomeAborted,
	}))

	// Verify nested dir was created.
	_, err = os.Stat(dir)
	assert.NoError(t, err)
}

func TestNoopCycleRecorder(t *testing.T) {
	t.Parallel()

	rec := &journal.NoopCycleRecorder{}
	assert.NoError(t, rec.Record(context.Background(), domain.CycleRecord{}))
	assert.NoError(t, rec.Close())
}

func TestJSONLCycleRecorder_ConcurrentWrites(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	rec, err := journal.NewJSONLCycleRecorder(dir)
	require.NoError(t, err)
	defer func() { _ = rec.Close() }()

	const numWriters = 10
	done := make(chan struct{}, numWriters)

	for i := range numWriters {
		go func(idx int) {
			defer func() { done <- struct{}{} }()
			record := domain.CycleRecord{
				ReqID:     "req-concurrent",
				CreatedAt: time.Now(),
				Outcome:   domain.OutcomeProfit,
			}
			_ = idx
			assert.NoError(t, rec.Record(context.Background(), record))
		}(i)
	}

	for range numWriters {
		<-done
	}

	// Verify all records were written.
	files, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, files, 1)

	data, err := os.ReadFile(filepath.Join(dir, files[0].Name()))
	require.NoError(t, err)

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	assert.Len(t, lines, numWriters)
}
