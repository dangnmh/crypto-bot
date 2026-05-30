package trap_test

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"crypto-bot/internal/bots/funding/application/trap"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/infrastructure/app"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrategyBasics(t *testing.T) {
	t.Parallel()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s := trap.NewStrategy(&app.Engine{}, &config.Config{}, logger)
	assert.Equal(t, trap.FlowTrap, s.Flow())
	assert.False(t, s.Enabled(config.SymbolConfig{}))
	require.NoError(t, s.Start(context.Background(), nil))
	require.NoError(t, s.Stop(context.Background()))
}
