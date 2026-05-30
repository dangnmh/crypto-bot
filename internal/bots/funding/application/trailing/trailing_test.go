package trailing_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application/trailing"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"
	"crypto-bot/internal/infrastructure/app"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrategyBasics(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := trailing.NewStrategy(&app.Engine{}, &config.Config{}, logger)

	assert.Equal(t, trailing.FlowTrailing, s.Flow())
	assert.False(t, s.Enabled(config.SymbolConfig{}))
	assert.True(t, s.Enabled(config.SymbolConfig{
		FundingTrap: domain.FundingTrapConfig{
			Enabled:  true,
			Trailing: domain.TrailingConfig{Enabled: true},
		},
	}))
	require.NoError(t, s.Start(context.Background(), nil))
	require.NoError(t, s.Stop(context.Background()))
}
