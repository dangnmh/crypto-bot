package app_test

import (
	"testing"
	"time"

	"crypto-bot/internal/infrastructure/app"
	"crypto-bot/internal/infrastructure/config"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
)

func TestFxBotRunnerLifecycle(t *testing.T) {
	t.Parallel()

	bot := &mockBot{}
	engine := app.NewEngine(app.EngineConfig{
		SystemConfig: &config.SystemConfig{},
	})

	fxApp := fxtest.New(
		t,
		fx.Provide(
			func() *app.Engine { return engine },
			func() app.Bot { return bot },
			app.NewBotRunner,
		),
		fx.Invoke(app.RegisterBotRunner),
	)

	fxApp.RequireStart()
	require.Eventually(t, func() bool {
		return bot.runStarted
	}, time.Second, 10*time.Millisecond)

	fxApp.RequireStop()
	assert.True(t, bot.runStopped)
}
