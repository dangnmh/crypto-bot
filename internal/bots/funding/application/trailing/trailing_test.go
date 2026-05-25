package trailing_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/trailing"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeClock struct {
	sleepErr error
	sleeps   int
}

func (c *fakeClock) Now() time.Time                       { return time.Unix(1, 0) }
func (c *fakeClock) Until(target time.Time) time.Duration { return time.Until(target) }
func (c *fakeClock) GetServerTime() int64                 { return c.Now().UnixMilli() }
func (c *fakeClock) LatencyMs() int64                     { return 1 }
func (c *fakeClock) Offset() int64                        { return 2 }
func (c *fakeClock) IsHealthy() bool                      { return true }
func (c *fakeClock) MsUntilTarget(targetServerTimeMs int64) int64 {
	return targetServerTimeMs - c.GetServerTime()
}
func (c *fakeClock) Sleep(context.Context, time.Duration) error { c.sleeps++; return c.sleepErr }

func TestStrategyBasics(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := trailing.NewStrategy(config.SymbolConfig{}, &config.Config{}, application.Deps{Log: logger, Clock: &fakeClock{}})

	assert.Equal(t, trailing.FlowTrailing, s.Flow())
	assert.False(t, s.Enabled(config.SymbolConfig{}))
	assert.True(t, s.Enabled(config.SymbolConfig{
		FundingTrap: domain.FundingTrapConfig{
			Enabled:  true,
			Trailing: domain.TrailingConfig{Enabled: true},
		},
	}))
	require.NoError(t, s.Execute(context.Background(), time.Now(), domain.Candidate{}))
	require.NoError(t, s.CleanupOpenExposure(context.Background()))
}

func TestRetryWithBackoff(t *testing.T) {
	t.Parallel()

	clock := &fakeClock{}
	s := trailing.NewStrategy(config.SymbolConfig{}, &config.Config{}, application.Deps{Clock: clock})
	attempts := 0

	gotAttempts, err := s.RetryWithBackoffOpts(context.Background(), 3, time.Nanosecond, time.Nanosecond, func() error {
		attempts++
		if attempts < 2 {
			return errors.New("try again")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 2, gotAttempts)
	assert.Equal(t, 1, clock.sleeps)
}

func TestRetryWithBackoffReturnsLastErrorAndSleepCancel(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("last")
	sleepErr := errors.New("cancel")
	clock := &fakeClock{sleepErr: sleepErr}
	s := trailing.NewStrategy(config.SymbolConfig{}, &config.Config{}, application.Deps{Clock: clock})

	attempts, err := s.RetryWithBackoffOpts(context.Background(), 3, time.Nanosecond, time.Nanosecond, func() error {
		return wantErr
	})
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, attempts)
}
