package strategy_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application/strategy"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeStrategy struct {
	flow     string
	enabled  bool
	execErr  error
	execN    int
	cleanupN int
}

func (s *fakeStrategy) Flow() string { return s.flow }
func (s *fakeStrategy) Enabled(config.SymbolConfig) bool {
	return s.enabled
}
func (s *fakeStrategy) Execute(context.Context, time.Time, domain.Candidate) error {
	s.execN++
	return s.execErr
}
func (s *fakeStrategy) CleanupOpenExposure(context.Context) error {
	s.cleanupN++
	return nil
}

func TestRegistryEnabledExecuteAndCleanup(t *testing.T) {
	t.Parallel()

	enabled := &fakeStrategy{flow: "enabled", enabled: true}
	disabled := &fakeStrategy{flow: "disabled", enabled: false}
	reg := strategy.NewRegistry(enabled, disabled)

	got := reg.Enabled(config.SymbolConfig{})
	require.Len(t, got, 1)
	assert.Equal(t, "enabled", got[0].Flow())

	err := reg.ExecuteAll(context.Background(), time.Now(), config.SymbolConfig{}, domain.Candidate{})
	require.NoError(t, err)
	assert.Equal(t, 1, enabled.execN)
	assert.Zero(t, disabled.execN)

	reg.CleanupOpenExposure(context.Background(), config.SymbolConfig{})
	assert.Equal(t, 1, enabled.cleanupN)
	assert.Zero(t, disabled.cleanupN)
}

func TestRegistryExecuteAllStopsOnFirstError(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("boom")
	first := &fakeStrategy{enabled: true, execErr: wantErr}
	second := &fakeStrategy{enabled: true}
	reg := strategy.NewRegistry(first, second)

	err := reg.ExecuteAll(context.Background(), time.Now(), config.SymbolConfig{}, domain.Candidate{})
	require.ErrorIs(t, err, wantErr)
	assert.Equal(t, 1, first.execN)
	assert.Zero(t, second.execN)
}
