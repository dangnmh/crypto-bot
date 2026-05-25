package trap_test

import (
	"context"
	"testing"
	"time"

	"crypto-bot/internal/bots/funding/application"
	"crypto-bot/internal/bots/funding/application/trap"
	"crypto-bot/internal/bots/funding/config"
	"crypto-bot/internal/bots/funding/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStrategyBasics(t *testing.T) {
	t.Parallel()

	s := trap.NewStrategy(config.SymbolConfig{}, &config.Config{}, application.Deps{})
	assert.Equal(t, trap.FlowTrap, s.Flow())
	assert.False(t, s.Enabled(config.SymbolConfig{}))
	require.NoError(t, s.Execute(context.Background(), time.Now(), domain.Candidate{}))
	require.NoError(t, s.CleanupOpenExposure(context.Background()))
}
